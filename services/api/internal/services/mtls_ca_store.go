package services

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	tpcrypto "github.com/BitaceS/talepanel/api/internal/crypto"
	"github.com/BitaceS/talepanel/api/internal/pki"
)

const (
	// mtlsCAKey holds the JSON-encoded panel CA (cert + key).
	mtlsCAKey = "daemon_mtls_ca"
	// mtlsPanelCertKey holds the JSON-encoded panel client cert (cert + key).
	mtlsPanelCertKey = "daemon_mtls_panel_cert"
	// mtlsPreviousCAKey holds the CA being replaced while a rotation runs.
	// Nodes still trust it and the panel still presents its client cert from
	// it, until every node has a certificate from the new CA.
	mtlsPreviousCAKey = "daemon_mtls_ca_previous"
	// panelClientCN is the common name of the panel's client certificate.
	panelClientCN = "panel"
	// panelCertMinRemaining is how long the stored panel client cert must
	// still be valid; a cert closer to expiry is reissued on load.
	panelCertMinRemaining = 30 * 24 * time.Hour
)

type pemPair struct {
	CertPEM []byte `json:"cert_pem"`
	KeyPEM  []byte `json:"key_pem"`
	// EndorsementPEM (CA pairs only): this CA's cross certificate signed by
	// the CA it replaced. Daemons accept a new root only with it.
	EndorsementPEM []byte `json:"endorsement_pem,omitempty"`
}

var (
	// ErrCARotationInProgress: a rotation was started and not finished yet.
	ErrCARotationInProgress = errors.New("a CA rotation is already in progress")
	// ErrNoCARotation: finish or cancel without a running rotation.
	ErrNoCARotation = errors.New("no CA rotation in progress")
)

// loadOrCreatePair returns the pair stored under key, creating it with gen on
// first use.  Creation is insert-if-absent followed by a re-read, so concurrent
// first callers all converge on whichever pair was stored first.
func (s *AppSettingsService) loadOrCreatePair(ctx context.Context, key string, gen func() (pemPair, error)) (pemPair, error) {
	read := func() (pemPair, bool, error) {
		raw, err := s.Get(ctx, key)
		if err != nil {
			return pemPair{}, false, err
		}
		if raw == "" {
			return pemPair{}, false, nil
		}
		var p pemPair
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return pemPair{}, false, fmt.Errorf("parsing %s: %w", key, err)
		}
		return p, true, nil
	}
	if p, ok, err := read(); err != nil || ok {
		return p, err
	}
	fresh, err := gen()
	if err != nil {
		return pemPair{}, err
	}
	data, err := json.Marshal(fresh)
	if err != nil {
		return pemPair{}, fmt.Errorf("encoding %s: %w", key, err)
	}
	if _, err := s.SetIfAbsent(ctx, key, string(data)); err != nil {
		return pemPair{}, err
	}
	p, ok, err := read()
	if err != nil {
		return pemPair{}, err
	}
	if !ok {
		return pemPair{}, fmt.Errorf("%s missing after store", key)
	}
	return p, nil
}

// MTLSCA loads the panel's mTLS CA, creating and persisting it on first use.
func (s *AppSettingsService) MTLSCA(ctx context.Context) (*pki.CA, error) {
	p, err := s.loadOrCreatePair(ctx, mtlsCAKey, func() (pemPair, error) {
		ca, err := pki.GenerateCA()
		if err != nil {
			return pemPair{}, err
		}
		return pemPair{CertPEM: ca.CertPEM, KeyPEM: ca.KeyPEM}, nil
	})
	if err != nil {
		return nil, err
	}
	return &pki.CA{CertPEM: p.CertPEM, KeyPEM: p.KeyPEM}, nil
}

// CACertPEM returns the public CA certificate, for handing to daemons.
func (s *AppSettingsService) CACertPEM(ctx context.Context) ([]byte, error) {
	ca, err := s.MTLSCA(ctx)
	if err != nil {
		return nil, err
	}
	return ca.CertPEM, nil
}

// PanelClientCert loads the panel's CN=panel client certificate and key,
// issuing and persisting it from the CA on first use. While a CA rotation
// runs it comes from the previous CA, which every node still trusts; after
// the rotation from the new one. A stored cert that no longer chains to that
// CA or has less than panelCertMinRemaining left is reissued and overwrites
// the stored one, so the panel never keeps presenting a cert the daemons
// reject.
func (s *AppSettingsService) PanelClientCert(ctx context.Context) (certPEM, keyPEM []byte, err error) {
	ca, err := s.panelSigningCA(ctx)
	if err != nil {
		return nil, nil, err
	}
	issue := func() (pemPair, error) {
		c, k, err := ca.IssueClientCert(panelClientCN)
		if err != nil {
			return pemPair{}, err
		}
		return pemPair{CertPEM: c, KeyPEM: k}, nil
	}
	p, err := s.loadOrCreatePair(ctx, mtlsPanelCertKey, issue)
	if err != nil {
		return nil, nil, err
	}
	if usablePanelCert(ca.CertPEM, p.CertPEM, time.Now()) {
		return p.CertPEM, p.KeyPEM, nil
	}
	fresh, err := issue()
	if err != nil {
		return nil, nil, err
	}
	data, err := json.Marshal(fresh)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding %s: %w", mtlsPanelCertKey, err)
	}
	if err := s.Set(ctx, mtlsPanelCertKey, string(data), nil); err != nil {
		return nil, nil, err
	}
	return fresh.CertPEM, fresh.KeyPEM, nil
}

// usablePanelCert reports whether certPEM chains to caPEM for client auth and
// stays valid for at least panelCertMinRemaining after now.
func usablePanelCert(caPEM, certPEM []byte, now time.Time) bool {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return false
	}
	blk, _ := pem.Decode(certPEM)
	if blk == nil {
		return false
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return false
	}
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: now,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		return false
	}
	return cert.NotAfter.Sub(now) >= panelCertMinRemaining
}

func (s *AppSettingsService) readPair(ctx context.Context, key string) (*pemPair, error) {
	raw, err := s.Get(ctx, key)
	if err != nil || raw == "" {
		return nil, err
	}
	var p pemPair
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", key, err)
	}
	return &p, nil
}

// panelSigningCA is the CA the panel's own client certificate comes from:
// the previous CA while a rotation runs, else the active one.
func (s *AppSettingsService) panelSigningCA(ctx context.Context) (*pki.CA, error) {
	prev, err := s.readPair(ctx, mtlsPreviousCAKey)
	if err != nil {
		return nil, err
	}
	if prev != nil {
		return &pki.CA{CertPEM: prev.CertPEM, KeyPEM: prev.KeyPEM}, nil
	}
	return s.MTLSCA(ctx)
}

// MTLSTrust is what nodes and the panel trust right now.
type MTLSTrust struct {
	// BundlePEM: the active CA first (it signs new node certs; older daemons
	// check their cert against the first CA only), then the previous one
	// while a rotation runs.
	BundlePEM []byte
	// EndorsementsPEM: the active CA's cross certificate from the CA it
	// replaced, if it replaced one.
	EndorsementsPEM []byte
	ActiveFP        string
	ActiveNotAfter  time.Time
	PreviousFP      string
	Rotating        bool
}

// MTLSTrust loads the current trust state, creating the CA on first use.
func (s *AppSettingsService) MTLSTrust(ctx context.Context) (*MTLSTrust, error) {
	ca, err := s.MTLSCA(ctx)
	if err != nil {
		return nil, err
	}
	active, err := s.readPair(ctx, mtlsCAKey)
	if err != nil {
		return nil, err
	}
	prev, err := s.readPair(ctx, mtlsPreviousCAKey)
	if err != nil {
		return nil, err
	}
	fp, err := pki.Fingerprint(ca.CertPEM)
	if err != nil {
		return nil, fmt.Errorf("active CA: %w", err)
	}
	_, notAfter, err := pki.CertInfo(ca.CertPEM)
	if err != nil {
		return nil, fmt.Errorf("active CA: %w", err)
	}
	t := &MTLSTrust{BundlePEM: append([]byte{}, ca.CertPEM...), ActiveFP: fp, ActiveNotAfter: notAfter}
	if active != nil && len(active.EndorsementPEM) > 0 {
		t.EndorsementsPEM = active.EndorsementPEM
	}
	if prev != nil {
		pfp, err := pki.Fingerprint(prev.CertPEM)
		if err != nil {
			return nil, fmt.Errorf("previous CA: %w", err)
		}
		t.PreviousFP = pfp
		t.Rotating = true
		t.BundlePEM = append(t.BundlePEM, prev.CertPEM...)
	}
	return t, nil
}

// setPairs writes several settings in one transaction; a nil pair deletes.
func (s *AppSettingsService) setPairs(ctx context.Context, by *uuid.UUID, pairs map[string]*pemPair) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for key, p := range pairs {
		if p == nil {
			if _, err := tx.Exec(ctx, `DELETE FROM app_settings WHERE key = $1`, key); err != nil {
				return fmt.Errorf("deleting %s: %w", key, err)
			}
			continue
		}
		data, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("encoding %s: %w", key, err)
		}
		enc, err := tpcrypto.Encrypt(s.encKey, data)
		if err != nil {
			return fmt.Errorf("encrypting %s: %w", key, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO app_settings (key, value, updated_at, updated_by)
			VALUES ($1, $2, NOW(), $3)
			ON CONFLICT (key) DO UPDATE
			   SET value = EXCLUDED.value, updated_at = NOW(), updated_by = EXCLUDED.updated_by
		`, key, enc, by); err != nil {
			return fmt.Errorf("storing %s: %w", key, err)
		}
	}
	return tx.Commit(ctx)
}

// StartCARotation creates a new CA and makes it the one that signs node
// certificates. The old CA stays trusted (and keeps signing the panel's
// client certificate) until FinishCARotation; the new CA carries a cross
// certificate from the old one so nodes accept it.
func (s *AppSettingsService) StartCARotation(ctx context.Context, by *uuid.UUID) error {
	prev, err := s.readPair(ctx, mtlsPreviousCAKey)
	if err != nil {
		return err
	}
	if prev != nil {
		return ErrCARotationInProgress
	}
	old, err := s.MTLSCA(ctx)
	if err != nil {
		return err
	}
	oldPair, err := s.readPair(ctx, mtlsCAKey)
	if err != nil {
		return err
	}
	if oldPair == nil {
		return errors.New("active CA missing")
	}
	fresh, err := pki.GenerateCA()
	if err != nil {
		return err
	}
	endorsement, err := old.Endorse(fresh.CertPEM)
	if err != nil {
		return err
	}
	return s.setPairs(ctx, by, map[string]*pemPair{
		mtlsPreviousCAKey: oldPair,
		mtlsCAKey:         {CertPEM: fresh.CertPEM, KeyPEM: fresh.KeyPEM, EndorsementPEM: endorsement},
	})
}

// FinishCARotation drops the previous CA and issues the panel's client
// certificate from the new one.
func (s *AppSettingsService) FinishCARotation(ctx context.Context, by *uuid.UUID) error {
	prev, err := s.readPair(ctx, mtlsPreviousCAKey)
	if err != nil {
		return err
	}
	if prev == nil {
		return ErrNoCARotation
	}
	ca, err := s.MTLSCA(ctx)
	if err != nil {
		return err
	}
	certPEM, keyPEM, err := ca.IssueClientCert(panelClientCN)
	if err != nil {
		return err
	}
	return s.setPairs(ctx, by, map[string]*pemPair{
		mtlsPreviousCAKey: nil,
		mtlsPanelCertKey:  {CertPEM: certPEM, KeyPEM: keyPEM},
	})
}

// CancelCARotation makes the previous CA active again and drops the new one.
// Nodes that already renewed are asked to renew again from the old CA.
func (s *AppSettingsService) CancelCARotation(ctx context.Context, by *uuid.UUID) error {
	prev, err := s.readPair(ctx, mtlsPreviousCAKey)
	if err != nil {
		return err
	}
	if prev == nil {
		return ErrNoCARotation
	}
	return s.setPairs(ctx, by, map[string]*pemPair{
		mtlsCAKey:         prev,
		mtlsPreviousCAKey: nil,
	})
}
