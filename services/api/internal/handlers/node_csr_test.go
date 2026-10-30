package handlers

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"encoding/pem"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/services"
)

type csrFixture struct {
	r      *gin.Engine
	pool   *pgxpool.Pool
	idA    string
	tokenA string
	idB    string
}

func newCSRFixture(t *testing.T, fqdnA string) *csrFixture {
	t.Helper()
	dbURL := os.Getenv("TALEPANEL_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TALEPANEL_TEST_DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	f := &csrFixture{pool: pool, idA: uuid.NewString(), idB: uuid.NewString(), tokenA: "csr-test-token-" + uuid.NewString()}
	tokenB := "csr-test-token-" + uuid.NewString()
	hash := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	for _, n := range []struct{ id, fqdn, tok string }{{f.idA, fqdnA, f.tokenA}, {f.idB, "other.example.com", tokenB}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO nodes (id, name, fqdn, port, token_hash) VALUES ($1, $2, $3, 8444, $4)`,
			n.id, "csr-test-"+n.id[:8], n.fqdn, hash(n.tok)); err != nil {
			t.Fatalf("seed node: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM nodes WHERE id IN ($1, $2)`, f.idA, f.idB)
		// The handler creates the panel CA on first use; don't leak it into
		// other packages' tests that share this database.
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM app_settings WHERE key IN ('daemon_mtls_ca', 'daemon_mtls_panel_cert')`)
	})

	settings := services.NewAppSettingsService(pool, bytes.Repeat([]byte{7}, 32))
	gin.SetMode(gin.TestMode)
	f.r = gin.New()
	g := f.r.Group("/nodes")
	g.Use(middleware.DaemonNodeAuth(pool))
	g.POST("/:id/csr", NewNodeCSRHandler(pool, settings).SignCSR)
	return f
}

func testCSRPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "attacker"},
		DNSNames: []string{"evil.example.com"},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func (f *csrFixture) post(t *testing.T, nodeID, token, csr string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"csr_pem": csr})
	req := httptest.NewRequest(http.MethodPost, "/nodes/"+nodeID+"/csr", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

func decodeCert(t *testing.T, s string) *x509.Certificate {
	t.Helper()
	b, _ := pem.Decode([]byte(s))
	if b == nil {
		t.Fatal("no PEM block")
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNodeCSR_signsForAuthedNode(t *testing.T) {
	f := newCSRFixture(t, "daemon.example.com")
	csr := testCSRPEM(t)
	for name, payload := range map[string]string{
		"pem":    string(csr),
		"base64": base64.StdEncoding.EncodeToString(csr),
	} {
		w := f.post(t, f.idA, f.tokenA, payload)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", name, w.Code, w.Body.String())
		}
		var resp struct {
			CACertPEM   string `json:"ca_cert_pem"`
			NodeCertPEM string `json:"node_cert_pem"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		leaf := decodeCert(t, resp.NodeCertPEM)
		if leaf.Subject.CommonName != f.idA {
			t.Fatalf("%s: CN = %q, want %q", name, leaf.Subject.CommonName, f.idA)
		}
		if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "daemon.example.com" {
			t.Fatalf("%s: SANs = %v", name, leaf.DNSNames)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(resp.CACertPEM)) {
			t.Fatal("bad CA pem")
		}
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "daemon.example.com", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
			t.Fatalf("%s: chain: %v", name, err)
		}

		var serial *string
		var notAfter *string
		if err := f.pool.QueryRow(context.Background(),
			`SELECT cert_serial, cert_not_after::text FROM nodes WHERE id = $1`, f.idA).Scan(&serial, &notAfter); err != nil {
			t.Fatal(err)
		}
		if serial == nil || *serial != leaf.SerialNumber.Text(16) || notAfter == nil {
			t.Fatalf("%s: persisted serial=%v notAfter=%v", name, serial, notAfter)
		}
	}
}

func TestNodeCSR_ipFQDNBecomesIPSAN(t *testing.T) {
	f := newCSRFixture(t, "203.0.113.7")
	w := f.post(t, f.idA, f.tokenA, string(testCSRPEM(t)))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		NodeCertPEM string `json:"node_cert_pem"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	leaf := decodeCert(t, resp.NodeCertPEM)
	if len(leaf.DNSNames) != 0 || len(leaf.IPAddresses) != 1 || leaf.IPAddresses[0].String() != "203.0.113.7" {
		t.Fatalf("DNS=%v IP=%v", leaf.DNSNames, leaf.IPAddresses)
	}
}

func TestNodeCSR_rejectsForeignNodeID(t *testing.T) {
	f := newCSRFixture(t, "daemon.example.com")
	w := f.post(t, f.idB, f.tokenA, string(testCSRPEM(t)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", w.Code)
	}
	var serial *string
	_ = f.pool.QueryRow(context.Background(), `SELECT cert_serial FROM nodes WHERE id = $1`, f.idB).Scan(&serial)
	if serial != nil {
		t.Fatal("foreign node row was modified")
	}
}

func TestNodeCSR_rejectsBadCSR(t *testing.T) {
	f := newCSRFixture(t, "daemon.example.com")
	for _, payload := range []string{"garbage", "", base64.StdEncoding.EncodeToString([]byte("garbage")), strings.Repeat("A", 100)} {
		w := f.post(t, f.idA, f.tokenA, payload)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("payload %q: status %d, want 400", payload, w.Code)
		}
	}
}

func TestNodeCSR_requiresAuth(t *testing.T) {
	f := newCSRFixture(t, "daemon.example.com")
	if w := f.post(t, f.idA, "wrong-token", string(testCSRPEM(t))); w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", w.Code)
	}
}
