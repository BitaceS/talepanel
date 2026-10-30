package daemon

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/pki"
)

const (
	testNodeID  = "6f1c2a7e-3b4d-4e5f-8a9b-0c1d2e3f4a5b"
	otherNodeID = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
)

func newTestCA(t *testing.T) *pki.CA {
	t.Helper()
	ca, err := pki.GenerateCA()
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

// signServerCert issues a daemon server cert from ca through the real CSR path.
func signServerCert(t *testing.T, ca *pki.CA, cn string, sans []string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}}, key)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
	certPEM, _, _, err := ca.SignCSR(csrPEM, cn, sans, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return keyPairFor(t, certPEM, key)
}

// signRawServerCert signs a leaf directly with the CA key so tests can control
// the validity window and extended key usages.
func signRawServerCert(t *testing.T, ca *pki.CA, cn string, notBefore, notAfter time.Time, eku []x509.ExtKeyUsage) tls.Certificate {
	t.Helper()
	cb, _ := pem.Decode(ca.CertPEM)
	caCert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := pem.Decode(ca.KeyPEM)
	caKey, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		DNSNames:              []string{"daemon"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           eku,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return keyPairFor(t, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), key)
}

func keyPairFor(t *testing.T, certPEM []byte, key *ecdsa.PrivateKey) tls.Certificate {
	t.Helper()
	kder, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kder}))
	if err != nil {
		t.Fatal(err)
	}
	return pair
}

// seen records what the mTLS daemon stub observed about the caller.
type seen struct {
	mu       sync.Mutex
	clientCN string
	auth     string
}

// startMTLSDaemon starts a daemon stub that serves serverCert and requires a
// client certificate chaining to panelCA.
func startMTLSDaemon(t *testing.T, panelCA *pki.CA, serverCert tls.Certificate) (*httptest.Server, *seen) {
	t.Helper()
	s := &seen{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			s.clientCN = r.TLS.PeerCertificates[0].Subject.CommonName
		}
		s.auth = r.Header.Get("Authorization")
		s.mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/backup/download") {
			_, _ = io.WriteString(w, "archive-bytes")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok","version":"test","node_id":"`+testNodeID+`"}`)
	}))
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(panelCA.CertPEM) {
		t.Fatal("bad CA pem")
	}
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
		MinVersion:   tls.VersionTLS12,
	}
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, s
}

func panelTLS(t *testing.T, ca *pki.CA, expectedCN string) *tls.Config {
	t.Helper()
	certPEM, keyPEM, err := ca.IssueClientCert("panel")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := BuildPanelTLSConfig(ca.CertPEM, certPEM, keyPEM, expectedCN)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func healthVia(t *testing.T, srv *httptest.Server, cfg *tls.Config) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := NewClientTLS(srv.URL, "tok", cfg, zap.NewNop()).Health(ctx)
	return err
}

func TestPanelTLS_acceptsCorrectCN(t *testing.T) {
	ca := newTestCA(t)
	srv, s := startMTLSDaemon(t, ca, signServerCert(t, ca, testNodeID, []string{"daemon"}))
	if err := healthVia(t, srv, panelTLS(t, ca, testNodeID)); err != nil {
		t.Fatalf("health over mTLS: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clientCN != "panel" {
		t.Fatalf("daemon saw client CN %q, want panel", s.clientCN)
	}
	if s.auth != "Bearer tok" {
		t.Fatalf("bearer header = %q", s.auth)
	}
}

func TestPanelTLS_rejectsWrongCN(t *testing.T) {
	ca := newTestCA(t)
	srv, _ := startMTLSDaemon(t, ca, signServerCert(t, ca, otherNodeID, []string{"daemon"}))
	if err := healthVia(t, srv, panelTLS(t, ca, testNodeID)); err == nil {
		t.Fatal("accepted a validly chained cert for another node")
	}
}

func TestPanelTLS_rejectsForeignCA(t *testing.T) {
	panelCA := newTestCA(t)
	foreign := newTestCA(t)
	// The daemon still trusts the panel CA for client certs, so only the
	// server-cert check can fail.
	srv, _ := startMTLSDaemon(t, panelCA, signServerCert(t, foreign, testNodeID, []string{"daemon"}))
	if err := healthVia(t, srv, panelTLS(t, panelCA, testNodeID)); err == nil {
		t.Fatal("accepted a cert from a foreign CA")
	}
}

func TestPanelTLS_rejectsExpiredLeaf(t *testing.T) {
	ca := newTestCA(t)
	now := time.Now()
	leaf := signRawServerCert(t, ca, testNodeID, now.Add(-2*time.Hour), now.Add(-time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	srv, _ := startMTLSDaemon(t, ca, leaf)
	if err := healthVia(t, srv, panelTLS(t, ca, testNodeID)); err == nil {
		t.Fatal("accepted an expired leaf")
	}
}

func TestPanelTLS_rejectsNotYetValidLeaf(t *testing.T) {
	ca := newTestCA(t)
	now := time.Now()
	leaf := signRawServerCert(t, ca, testNodeID, now.Add(time.Hour), now.Add(2*time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	srv, _ := startMTLSDaemon(t, ca, leaf)
	if err := healthVia(t, srv, panelTLS(t, ca, testNodeID)); err == nil {
		t.Fatal("accepted a not-yet-valid leaf")
	}
}

func TestPanelTLS_rejectsClientOnlyLeaf(t *testing.T) {
	ca := newTestCA(t)
	now := time.Now()
	leaf := signRawServerCert(t, ca, testNodeID, now.Add(-time.Hour), now.Add(time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	srv, _ := startMTLSDaemon(t, ca, leaf)
	if err := healthVia(t, srv, panelTLS(t, ca, testNodeID)); err == nil {
		t.Fatal("accepted a leaf without serverAuth usage")
	}
}

func TestPanelTLS_ignoresHostname(t *testing.T) {
	ca := newTestCA(t)
	// No IP SAN: the stub is reached via 127.0.0.1 but only names "daemon".
	srv, _ := startMTLSDaemon(t, ca, signServerCert(t, ca, testNodeID, []string{"daemon"}))
	if !strings.HasPrefix(srv.URL, "https://127.0.0.1:") {
		t.Fatalf("unexpected test server URL %s", srv.URL)
	}
	if err := healthVia(t, srv, panelTLS(t, ca, testNodeID)); err != nil {
		t.Fatalf("hostname mismatch should be ignored: %v", err)
	}
}

func TestPanelTLS_emptyPeerChainErrors(t *testing.T) {
	ca := newTestCA(t)
	cfg := panelTLS(t, ca, testNodeID)
	// The pin lives in VerifyConnection, which (unlike VerifyPeerCertificate)
	// also runs on resumed sessions.
	if cfg.VerifyConnection == nil {
		t.Fatal("no connection verifier set")
	}
	if cfg.VerifyPeerCertificate != nil {
		t.Fatal("verification must not depend on VerifyPeerCertificate")
	}
	if err := cfg.VerifyConnection(tls.ConnectionState{}); err == nil {
		t.Fatal("empty peer chain accepted")
	}
	if cfg.MinVersion < tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x", cfg.MinVersion)
	}
}

func TestBuildPanelTLSConfig_rejectsBadInput(t *testing.T) {
	ca := newTestCA(t)
	certPEM, keyPEM, err := ca.IssueClientCert("panel")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPanelTLSConfig(ca.CertPEM, certPEM, keyPEM, ""); err == nil {
		t.Fatal("empty expected CN accepted")
	}
	if _, err := BuildPanelTLSConfig([]byte("junk"), certPEM, keyPEM, testNodeID); err == nil {
		t.Fatal("junk CA accepted")
	}
	if _, err := BuildPanelTLSConfig(ca.CertPEM, certPEM, []byte("junk"), testNodeID); err == nil {
		t.Fatal("junk key accepted")
	}
}

// Long-running calls (backups, downloads) use a separate no-timeout client;
// it must carry the same mTLS transport.
func TestPanelTLS_longClientUsesTLS(t *testing.T) {
	ca := newTestCA(t)
	srv, _ := startMTLSDaemon(t, ca, signServerCert(t, ca, testNodeID, []string{"daemon"}))
	c := NewClientTLS(srv.URL, "tok", panelTLS(t, ca, testNodeID), zap.NewNop())
	rc, err := c.DownloadBackup(context.Background(), "sid", "bid")
	if err != nil {
		t.Fatalf("download over mTLS: %v", err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "archive-bytes" {
		t.Fatalf("body = %q", b)
	}
}

func TestClientPool_registerPicksScheme(t *testing.T) {
	ca := newTestCA(t)
	certPEM, keyPEM, err := ca.IssueClientCert("panel")
	if err != nil {
		t.Fatal(err)
	}

	// No TLS material: an mTLS node falls back to legacy http.
	p := NewClientPool(zap.NewNop())
	if err := p.Register(testNodeID, "daemon", 8444, "t", true); err != nil {
		t.Fatal(err)
	}
	if c, _ := p.Get(testNodeID); c == nil || c.baseURL != "http://daemon:8444" || c.tls {
		t.Fatalf("fallback client: %+v", c)
	}

	if err := p.SetTLSMaterial(ca.CertPEM, certPEM, keyPEM); err != nil {
		t.Fatal(err)
	}
	if err := p.Register(testNodeID, "daemon", 8444, "t", true); err != nil {
		t.Fatal(err)
	}
	c, _ := p.Get(testNodeID)
	if c == nil || c.baseURL != "https://daemon:8444" || !c.tls {
		t.Fatalf("mtls client: %+v", c)
	}
	// An unchanged registration keeps the existing client (and its pooled
	// connections) instead of rebuilding it every heartbeat.
	if err := p.Register(testNodeID, "daemon", 8444, "t", true); err != nil {
		t.Fatal(err)
	}
	if c2, _ := p.Get(testNodeID); c2 != c {
		t.Fatal("unchanged registration rebuilt the client")
	}

	if err := p.Register(otherNodeID, "daemon", 8444, "t", false); err != nil {
		t.Fatal(err)
	}
	if c, _ := p.Get(otherNodeID); c == nil || c.baseURL != "http://daemon:8444" || c.tls {
		t.Fatalf("legacy client: %+v", c)
	}

	if err := p.SetTLSMaterial([]byte("junk"), certPEM, keyPEM); err == nil {
		t.Fatal("junk TLS material accepted")
	}
}

// End to end through the pool: the registered client verifies the node's CN.
func TestClientPool_mtlsClientPinsNodeCN(t *testing.T) {
	ca := newTestCA(t)
	certPEM, keyPEM, err := ca.IssueClientCert("panel")
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := startMTLSDaemon(t, ca, signServerCert(t, ca, testNodeID, []string{"daemon"}))
	_, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	portN, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	p := NewClientPool(zap.NewNop())
	if err := p.SetTLSMaterial(ca.CertPEM, certPEM, keyPEM); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		nodeID string
		ok     bool
	}{{testNodeID, true}, {otherNodeID, false}} {
		if err := p.Register(tc.nodeID, "127.0.0.1", portN, "t", true); err != nil {
			t.Fatal(err)
		}
		c, _ := p.Get(tc.nodeID)
		_, err := c.Health(context.Background())
		if (err == nil) != tc.ok {
			t.Fatalf("node %s: err=%v want ok=%v", tc.nodeID, err, tc.ok)
		}
	}
}
