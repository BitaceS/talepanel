package services

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
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
