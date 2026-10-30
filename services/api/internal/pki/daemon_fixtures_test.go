package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Cross-language interop fixtures for the Rust daemon's tests
// (services/daemon/testdata/go-pki). Skipped unless
// TALEPANEL_DAEMON_FIXTURES_DIR names the output directory (absolute, since
// go test runs in the package directory). From services/api:
//
//	TALEPANEL_DAEMON_FIXTURES_DIR=$PWD/../daemon/testdata/go-pki go test ./internal/pki -run TestWriteDaemonInteropFixtures
//
// Everything is issued through this package's real signing paths; only the
// validity is stretched to ~20 years so the committed PEMs do not rot.
const (
	fixtureNodeID      = "11111111-2222-3333-4444-555555555555"
	fixtureOtherNodeID = "99999999-8888-7777-6666-555555555555"
	fixtureCAValidity  = 20 * 365 * 24 * time.Hour
	fixtureLeafValid   = 19 * 365 * 24 * time.Hour
)

func TestWriteDaemonInteropFixtures(t *testing.T) {
	dir := os.Getenv("TALEPANEL_DAEMON_FIXTURES_DIR")
	if dir == "" {
		t.Skip("TALEPANEL_DAEMON_FIXTURES_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ca, err := generateCA(fixtureCAValidity)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A daemon-style keypair + CSR, signed via SignCSR exactly as
	// POST /nodes/:id/csr does (CN forced to the node id, SAN = address).
	signed := func(cn string, sans []string) (certPEM, keyPEM []byte) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.CreateCertificateRequest(rand.Reader,
			&x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}}, key)
		if err != nil {
			t.Fatal(err)
		}
		csr := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
		certPEM, _, _, err = ca.SignCSR(csr, cn, sans, fixtureLeafValid)
		if err != nil {
			t.Fatal(err)
		}
		keyPEM, err = encodeKey(key)
		if err != nil {
			t.Fatal(err)
		}
		return certPEM, keyPEM
	}
	// The panel client cert takes IssueClientCert's path (sign with CN
	// "panel", no SANs), with the longer validity.
	panelKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	panelCert, _, _, err := ca.sign(&panelKey.PublicKey, "panel", nil, fixtureLeafValid)
	if err != nil {
		t.Fatal(err)
	}
	panelKeyPEM, err := encodeKey(panelKey)
	if err != nil {
		t.Fatal(err)
	}

	nodeCert, nodeKey := signed(fixtureNodeID, []string{"127.0.0.1"})
	otherCert, otherKey := signed(fixtureOtherNodeID, []string{"127.0.0.1"})

	write("ca.pem", ca.CertPEM)
	write("panel-cert.pem", panelCert)
	write("panel-key.pem", panelKeyPEM)
	write("node-cert.pem", nodeCert)
	write("node-key.pem", nodeKey)
	write("other-node-cert.pem", otherCert)
	write("other-node-key.pem", otherKey)
}
