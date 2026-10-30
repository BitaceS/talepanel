package services

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BitaceS/talepanel/api/internal/pki"
)

func mtlsTestService(t *testing.T) (*AppSettingsService, *pgxpool.Pool) {
	t.Helper()
	pool := openTestDB(t)
	clean := func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM app_settings WHERE key IN ('daemon_mtls_ca','daemon_mtls_panel_cert')`)
	}
	clean()
	t.Cleanup(clean)
	return NewAppSettingsService(pool, bytes.Repeat([]byte{7}, 32)), pool
}

func TestMTLSCA_createsThenReloadsSame(t *testing.T) {
	svc, _ := mtlsTestService(t)
	ctx := context.Background()
	a, err := svc.MTLSCA(ctx)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := svc.MTLSCA(ctx)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !bytes.Equal(a.CertPEM, b.CertPEM) || !bytes.Equal(a.KeyPEM, b.KeyPEM) {
		t.Fatal("CA changed between calls")
	}
	pub, err := svc.CACertPEM(ctx)
	if err != nil || !bytes.Equal(pub, a.CertPEM) {
		t.Fatalf("CACertPEM mismatch: %v", err)
	}
}

func TestMTLSCA_missingEncKeyErrors(t *testing.T) {
	_, pool := mtlsTestService(t)
	svc := NewAppSettingsService(pool, nil)
	if _, err := svc.MTLSCA(context.Background()); err == nil {
		t.Fatal("expected error without enc key")
	}
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM app_settings WHERE key = 'daemon_mtls_ca'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected nothing persisted, got %d rows", n)
	}
}

func TestMTLSCA_concurrentFirstUseConverges(t *testing.T) {
	svc, _ := mtlsTestService(t)
	const n = 8
	certs := make([][]byte, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ca, err := svc.MTLSCA(context.Background())
			errs[i] = err
			if err == nil {
				certs[i] = ca.CertPEM
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if !bytes.Equal(certs[i], certs[0]) {
			t.Fatalf("goroutine %d got a different CA", i)
		}
	}
}

func TestPanelClientCert_chainsToCA(t *testing.T) {
	svc, _ := mtlsTestService(t)
	ctx := context.Background()
	certPEM, keyPEM, err := svc.PanelClientCert(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keyPEM) == 0 {
		t.Fatal("empty key")
	}
	again, _, err := svc.PanelClientCert(ctx)
	if err != nil || !bytes.Equal(again, certPEM) {
		t.Fatalf("panel cert not stable: %v", err)
	}
	caPEM, err := svc.CACertPEM(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("bad CA pem")
	}
	blk, _ := pem.Decode(certPEM)
	if blk == nil {
		t.Fatal("bad cert pem")
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "panel" {
		t.Fatalf("CN = %q", cert.Subject.CommonName)
	}
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func parsePEMCert(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	blk, _ := pem.Decode(certPEM)
	if blk == nil {
		t.Fatal("bad cert pem")
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// storePanelCert overwrites the stored panel client cert with one signed by
// ca and valid for validFor.
func storePanelCert(t *testing.T, svc *AppSettingsService, ca *pki.CA, validFor time.Duration) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "panel"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	certPEM, _, _, err := ca.SignCSR(csrPEM, "panel", nil, validFor)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(pemPair{CertPEM: certPEM, KeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})})
	if err := svc.Set(context.Background(), mtlsPanelCertKey, string(data), nil); err != nil {
		t.Fatal(err)
	}
	return certPEM
}

func assertPanelCertReissued(t *testing.T, svc *AppSettingsService, stale []byte) {
	t.Helper()
	ctx := context.Background()
	certPEM, _, err := svc.PanelClientCert(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(certPEM, stale) {
		t.Fatal("stale panel cert was not reissued")
	}
	caPEM, err := svc.CACertPEM(ctx)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caPEM)
	cert := parsePEMCert(t, certPEM)
	if _, err := cert.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("reissued cert does not chain to the current CA: %v", err)
	}
	if time.Until(cert.NotAfter) < panelCertMinRemaining {
		t.Fatalf("reissued cert expires too soon: %v", cert.NotAfter)
	}
	// The replacement is persisted, not reissued on every call.
	again, _, err := svc.PanelClientCert(ctx)
	if err != nil || !bytes.Equal(again, certPEM) {
		t.Fatalf("reissued panel cert not persisted: %v", err)
	}
}

func TestPanelClientCert_reissuedWhenCAChanged(t *testing.T) {
	svc, _ := mtlsTestService(t)
	if _, err := svc.MTLSCA(context.Background()); err != nil {
		t.Fatal(err)
	}
	other, err := pki.GenerateCA()
	if err != nil {
		t.Fatal(err)
	}
	assertPanelCertReissued(t, svc, storePanelCert(t, svc, other, 2*365*24*time.Hour))
}

func TestPanelClientCert_reissuedNearExpiry(t *testing.T) {
	svc, _ := mtlsTestService(t)
	ca, err := svc.MTLSCA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertPanelCertReissued(t, svc, storePanelCert(t, svc, ca, 10*24*time.Hour))
}
