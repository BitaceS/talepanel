package services

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/BitaceS/talepanel/api/internal/pki"
)

const (
	// mtlsCAKey holds the JSON-encoded panel CA (cert + key).
	mtlsCAKey = "daemon_mtls_ca"
	// mtlsPanelCertKey holds the JSON-encoded panel client cert (cert + key).
	mtlsPanelCertKey = "daemon_mtls_panel_cert"
	// panelClientCN is the common name of the panel's client certificate.
	panelClientCN = "panel"
	// panelCertMinRemaining is how long the stored panel client cert must
	// still be valid; a cert closer to expiry is reissued on load.
	panelCertMinRemaining = 30 * 24 * time.Hour
)

type pemPair struct {
	CertPEM []byte `json:"cert_pem"`
	KeyPEM  []byte `json:"key_pem"`
}

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
// issuing and persisting it from the CA on first use. A stored cert that no
// longer chains to the current CA (CA replaced) or has less than
// panelCertMinRemaining left is reissued and overwrites the stored one, so
// the panel never keeps presenting a cert the daemons reject.
func (s *AppSettingsService) PanelClientCert(ctx context.Context) (certPEM, keyPEM []byte, err error) {
	ca, err := s.MTLSCA(ctx)
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
