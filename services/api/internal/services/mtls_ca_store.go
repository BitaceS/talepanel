package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/BitaceS/talepanel/api/internal/pki"
)

const (
	// mtlsCAKey holds the JSON-encoded panel CA (cert + key).
	mtlsCAKey = "daemon_mtls_ca"
	// mtlsPanelCertKey holds the JSON-encoded panel client cert (cert + key).
	mtlsPanelCertKey = "daemon_mtls_panel_cert"
	// panelClientCN is the common name of the panel's client certificate.
	panelClientCN = "panel"
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
// issuing and persisting it from the CA on first use.
func (s *AppSettingsService) PanelClientCert(ctx context.Context) (certPEM, keyPEM []byte, err error) {
	ca, err := s.MTLSCA(ctx)
	if err != nil {
		return nil, nil, err
	}
	p, err := s.loadOrCreatePair(ctx, mtlsPanelCertKey, func() (pemPair, error) {
		c, k, err := ca.IssueClientCert(panelClientCN)
		if err != nil {
			return pemPair{}, err
		}
		return pemPair{CertPEM: c, KeyPEM: k}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return p.CertPEM, p.KeyPEM, nil
}
