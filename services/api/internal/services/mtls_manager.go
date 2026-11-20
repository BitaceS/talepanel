package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/daemon"
	"github.com/BitaceS/talepanel/api/internal/pki"
)

// ErrNodesNotRenewed: finishing a CA rotation would cut off nodes that do not
// have a certificate from the new CA yet.
var ErrNodesNotRenewed = errors.New("some nodes have no certificate from the new CA yet")

// panelCertRefreshInterval is how often the panel re-checks its own client
// certificate (reissued when less than 30 days are left).
const panelCertRefreshInterval = 12 * time.Hour

// MTLSManager keeps the panel's mTLS trust (CA bundle, panel client
// certificate) in the daemon client pool up to date and runs CA rotations.
type MTLSManager struct {
	db       *pgxpool.Pool
	settings *AppSettingsService
	pool     *daemon.ClientPool
	log      *zap.Logger

	mu    sync.RWMutex
	trust *MTLSTrust
	// panelCertNotAfter is the expiry of the client certificate in use.
	panelCertNotAfter time.Time
	// opMu serialises rotation steps within this process.
	opMu sync.Mutex
}

func NewMTLSManager(db *pgxpool.Pool, settings *AppSettingsService, pool *daemon.ClientPool, log *zap.Logger) *MTLSManager {
	if log == nil {
		log = zap.NewNop()
	}
	return &MTLSManager{db: db, settings: settings, pool: pool, log: log}
}

// Refresh reloads the trust state and the panel client certificate (issuing
// a new one if it is close to expiry or from the wrong CA) and hands both to
// the daemon client pool.
func (m *MTLSManager) Refresh(ctx context.Context) error {
	trust, err := m.settings.MTLSTrust(ctx)
	if err != nil {
		return fmt.Errorf("loading mTLS trust: %w", err)
	}
	certPEM, keyPEM, err := m.settings.PanelClientCert(ctx)
	if err != nil {
		return fmt.Errorf("loading panel client certificate: %w", err)
	}
	if m.pool != nil {
		if err := m.pool.SetTLSMaterial(trust.BundlePEM, certPEM, keyPEM); err != nil {
			return fmt.Errorf("applying mTLS material: %w", err)
		}
	}
	_, notAfter, _ := pki.CertInfo(certPEM)
	m.mu.Lock()
	m.trust = trust
	m.panelCertNotAfter = notAfter
	m.mu.Unlock()
	return nil
}

// Trust returns the cached trust state (nil before the first Refresh).
func (m *MTLSManager) Trust() *MTLSTrust {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.trust
}

// BackfillFingerprints marks node certificates issued before the panel
// recorded issuing CAs as coming from the active CA. Only safe while no
// rotation runs: then there is only one CA they can come from.
func (m *MTLSManager) BackfillFingerprints(ctx context.Context) error {
	t := m.Trust()
	if t == nil || t.Rotating {
		return nil
	}
	_, err := m.db.Exec(ctx, `
		UPDATE nodes SET cert_ca_fingerprint = $1
		WHERE cert_ca_fingerprint IS NULL AND cert_serial IS NOT NULL
	`, t.ActiveFP)
	return err
}

// Run refreshes the panel client certificate periodically until ctx ends,
// so it is renewed while the API keeps running.
func (m *MTLSManager) Run(ctx context.Context) {
	ticker := time.NewTicker(panelCertRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.Refresh(ctx); err != nil {
				m.log.Warn("refreshing panel mTLS material failed", zap.Error(err))
			}
		}
	}
}

// PKIHint is what a heartbeat answer tells a daemon about its certificate.
type PKIHint struct {
	// CABundlePEM is what the node should trust from now on.
	CABundlePEM string `json:"ca_bundle_pem"`
	// EndorsementsPEM proves new CAs in the bundle to a node that trusts an
	// older one.
	EndorsementsPEM string `json:"endorsements_pem,omitempty"`
	// Renew: the node's certificate is not from the active CA; it should get
	// a new one now.
	Renew bool `json:"renew"`
}

// HeartbeatHint returns the hint for a node, or nil when the node does not
// use mTLS or the trust state is not loaded.
func (m *MTLSManager) HeartbeatHint(ctx context.Context, nodeID uuid.UUID) (*PKIHint, error) {
	t := m.Trust()
	if t == nil {
		return nil, nil
	}
	var mtls bool
	var fp *string
	if err := m.db.QueryRow(ctx,
		`SELECT mtls_enabled, cert_ca_fingerprint FROM nodes WHERE id = $1`, nodeID,
	).Scan(&mtls, &fp); err != nil {
		return nil, err
	}
	if !mtls {
		return nil, nil
	}
	return &PKIHint{
		CABundlePEM:     string(t.BundlePEM),
		EndorsementsPEM: string(t.EndorsementsPEM),
		Renew:           fp == nil || *fp != t.ActiveFP,
	}, nil
}

// MTLSNodeState is one node's certificate state in the admin overview.
type MTLSNodeState struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	MTLSEnabled    bool       `json:"mtls_enabled"`
	CertNotAfter   *time.Time `json:"cert_not_after,omitempty"`
	OnActiveCA     bool       `json:"on_active_ca"`
	Serving        bool       `json:"serving"`
	RenewalSupport bool       `json:"renewal_supported"`
	LastHeartbeat  *time.Time `json:"last_heartbeat,omitempty"`
}

// Renewed reports whether the node serves a certificate from the active CA.
func (n MTLSNodeState) Renewed() bool {
	return !n.MTLSEnabled || (n.OnActiveCA && n.Serving)
}

// MTLSStatus is the admin overview of the panel's certificates.
type MTLSStatus struct {
	CAFingerprint       string          `json:"ca_fingerprint"`
	CANotAfter          time.Time       `json:"ca_not_after"`
	Rotating            bool            `json:"rotating"`
	PreviousFingerprint string          `json:"previous_ca_fingerprint,omitempty"`
	PanelCertNotAfter   time.Time       `json:"panel_cert_not_after"`
	Nodes               []MTLSNodeState `json:"nodes"`
	NodesPendingRenewal int             `json:"nodes_pending_renewal"`
	NodesWithoutRenewal int             `json:"nodes_without_renewal"`
}

// Status returns the overview.
func (m *MTLSManager) Status(ctx context.Context) (*MTLSStatus, error) {
	if m.Trust() == nil {
		if err := m.Refresh(ctx); err != nil {
			return nil, err
		}
	}
	t := m.Trust()
	m.mu.RLock()
	panelNotAfter := m.panelCertNotAfter
	m.mu.RUnlock()

	rows, err := m.db.Query(ctx, `
		SELECT id, name, status::text, mtls_enabled, cert_not_after,
		       COALESCE(cert_ca_fingerprint = $1, false),
		       COALESCE(served_cert_serial = cert_serial, false),
		       cert_renewal_supported, last_heartbeat
		FROM nodes ORDER BY name
	`, t.ActiveFP)
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	defer rows.Close()
	st := &MTLSStatus{
		CAFingerprint:       t.ActiveFP,
		CANotAfter:          t.ActiveNotAfter,
		Rotating:            t.Rotating,
		PreviousFingerprint: t.PreviousFP,
		PanelCertNotAfter:   panelNotAfter,
		Nodes:               []MTLSNodeState{},
	}
	for rows.Next() {
		var n MTLSNodeState
		if err := rows.Scan(&n.ID, &n.Name, &n.Status, &n.MTLSEnabled, &n.CertNotAfter,
			&n.OnActiveCA, &n.Serving, &n.RenewalSupport, &n.LastHeartbeat); err != nil {
			return nil, fmt.Errorf("scanning node: %w", err)
		}
		if !n.Renewed() {
			st.NodesPendingRenewal++
		}
		if n.MTLSEnabled && !n.RenewalSupport {
			st.NodesWithoutRenewal++
		}
		st.Nodes = append(st.Nodes, n)
	}
	return st, rows.Err()
}

// StartRotation creates a new CA; nodes are asked to renew on their next
// heartbeat.
func (m *MTLSManager) StartRotation(ctx context.Context, by *uuid.UUID) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if err := m.settings.StartCARotation(ctx, by); err != nil {
		return err
	}
	return m.Refresh(ctx)
}

// FinishRotation retires the previous CA. Without force it refuses while a
// node with mTLS has no certificate from the new CA, because that node would
// be cut off until it renews.
func (m *MTLSManager) FinishRotation(ctx context.Context, by *uuid.UUID, force bool) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if !force {
		st, err := m.Status(ctx)
		if err != nil {
			return err
		}
		if !st.Rotating {
			return ErrNoCARotation
		}
		if st.NodesPendingRenewal > 0 {
			return fmt.Errorf("%w (%d left)", ErrNodesNotRenewed, st.NodesPendingRenewal)
		}
	}
	if err := m.settings.FinishCARotation(ctx, by); err != nil {
		return err
	}
	return m.Refresh(ctx)
}

// CancelRotation goes back to the previous CA.
func (m *MTLSManager) CancelRotation(ctx context.Context, by *uuid.UUID) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if err := m.settings.CancelCARotation(ctx, by); err != nil {
		return err
	}
	return m.Refresh(ctx)
}
