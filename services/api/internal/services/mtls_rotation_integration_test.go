package services

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/daemon"
	"github.com/BitaceS/talepanel/api/internal/pki"
)

func rotationTestService(t *testing.T) (*AppSettingsService, *pgxpool.Pool) {
	t.Helper()
	pool := openTestDB(t)
	clean := func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM app_settings WHERE key IN ('daemon_mtls_ca','daemon_mtls_panel_cert','daemon_mtls_ca_previous')`)
	}
	clean()
	t.Cleanup(clean)
	return NewAppSettingsService(pool, bytes.Repeat([]byte{7}, 32)), pool
}

func certsIn(t *testing.T, bundle []byte) []*x509.Certificate {
	t.Helper()
	var out []*x509.Certificate
	for rest := bundle; ; {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			return out
		}
		c, err := x509.ParseCertificate(b.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
}

func chainsTo(t *testing.T, certPEM, caPEM []byte) bool {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caPEM)
	_, err := certsIn(t, certPEM)[0].Verify(x509.VerifyOptions{
		Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	return err == nil
}

// signNode simulates POST /nodes/:id/csr: sign with the active CA and record
// serial + issuing CA, then the heartbeat that confirms the node serves it.
func signNode(t *testing.T, s *AppSettingsService, pool *pgxpool.Pool, id uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	ca, err := s.MTLSCA(ctx)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := s.MTLSTrust(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, serial, notAfter, err := ca.SignCSR(newTestCSR(t, id.String()), id.String(), []string{"daemon"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE nodes SET cert_serial = $2, cert_not_after = $3, cert_ca_fingerprint = $4 WHERE id = $1`,
		id, serial, notAfter, trust.ActiveFP); err != nil {
		t.Fatal(err)
	}
	if _, err := NewNodeService(pool).UpdateNodeHeartbeat(ctx, id, NodeHeartbeatRequest{
		Status: "online", MTLSReady: true, CertSerial: serial, CertNotAfter: &notAfter, CertRenewal: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCARotation_lifecycle(t *testing.T) {
	s, pool := rotationTestService(t)
	ctx := context.Background()
	mgr := NewMTLSManager(pool, s, daemon.NewClientPool(zap.NewNop()), zap.NewNop())
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	before := mgr.Trust()
	if before.Rotating || len(certsIn(t, before.BundlePEM)) != 1 {
		t.Fatalf("fresh trust: %+v", before)
	}
	oldCAPEM := append([]byte{}, before.BundlePEM...)

	node := seedHeartbeatNode(t, pool, nil)
	signNode(t, s, pool, node)
	if h, err := mgr.HeartbeatHint(ctx, node); err != nil || h == nil || h.Renew {
		t.Fatalf("hint before rotation: %+v %v", h, err)
	}

	// Start: new CA active, old one still trusted and signing the panel cert.
	if err := mgr.StartRotation(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.StartRotation(ctx, nil); !errors.Is(err, ErrCARotationInProgress) {
		t.Fatalf("second start: %v", err)
	}
	mid := mgr.Trust()
	certs := certsIn(t, mid.BundlePEM)
	if !mid.Rotating || len(certs) != 2 || mid.ActiveFP == before.ActiveFP || mid.PreviousFP != before.ActiveFP {
		t.Fatalf("mid trust: %+v", mid)
	}
	if fp, _ := pki.Fingerprint(mid.BundlePEM); fp != mid.ActiveFP {
		t.Fatal("active CA is not first in the bundle")
	}
	cross := certsIn(t, mid.EndorsementsPEM)
	if len(cross) != 1 || cross[0].CheckSignatureFrom(certs[1]) != nil ||
		!bytes.Equal(cross[0].RawSubjectPublicKeyInfo, certs[0].RawSubjectPublicKeyInfo) {
		t.Fatal("new CA not endorsed by the old one")
	}
	panelCert, _, err := s.PanelClientCert(ctx)
	if err != nil || !chainsTo(t, panelCert, oldCAPEM) {
		t.Fatalf("panel cert must stay on the old CA during rotation: %v", err)
	}

	h, _ := mgr.HeartbeatHint(ctx, node)
	if h == nil || !h.Renew || h.CABundlePEM != string(mid.BundlePEM) || h.EndorsementsPEM == "" {
		t.Fatalf("hint during rotation: %+v", h)
	}
	if err := mgr.FinishRotation(ctx, nil, false); !errors.Is(err, ErrNodesNotRenewed) {
		t.Fatalf("finish with a pending node: %v", err)
	}
	st, err := mgr.Status(ctx)
	if err != nil || st.NodesPendingRenewal < 1 {
		t.Fatalf("status: %+v %v", st, err)
	}

	// The node renews from the new CA; then the rotation can finish.
	signNode(t, s, pool, node)
	if h, _ := mgr.HeartbeatHint(ctx, node); h == nil || h.Renew {
		t.Fatalf("hint after renewal: %+v", h)
	}
	// Other tests share the DB; only this test's node must be renewed.
	_, _ = pool.Exec(ctx, `UPDATE nodes SET cert_ca_fingerprint = $1, served_cert_serial = cert_serial WHERE mtls_enabled AND id <> $2`, mid.ActiveFP, node)
	if err := mgr.FinishRotation(ctx, nil, false); err != nil {
		t.Fatalf("finish: %v", err)
	}
	after := mgr.Trust()
	if after.Rotating || len(certsIn(t, after.BundlePEM)) != 1 || after.ActiveFP != mid.ActiveFP {
		t.Fatalf("after trust: %+v", after)
	}
	panelCert, _, _ = s.PanelClientCert(ctx)
	if !chainsTo(t, panelCert, after.BundlePEM) || chainsTo(t, panelCert, oldCAPEM) {
		t.Fatal("panel cert not reissued from the new CA")
	}
	if err := mgr.FinishRotation(ctx, nil, false); !errors.Is(err, ErrNoCARotation) {
		t.Fatalf("finish twice: %v", err)
	}
}

func TestCARotation_cancelRestoresOldCA(t *testing.T) {
	s, pool := rotationTestService(t)
	ctx := context.Background()
	mgr := NewMTLSManager(pool, s, nil, zap.NewNop())
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	old := mgr.Trust()
	if err := mgr.CancelRotation(ctx, nil); !errors.Is(err, ErrNoCARotation) {
		t.Fatalf("cancel without rotation: %v", err)
	}
	if err := mgr.StartRotation(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.CancelRotation(ctx, nil); err != nil {
		t.Fatal(err)
	}
	back := mgr.Trust()
	if back.Rotating || back.ActiveFP != old.ActiveFP || !bytes.Equal(back.BundlePEM, old.BundlePEM) {
		t.Fatalf("cancel did not restore the old CA: %+v", back)
	}
	// Forced finish works without any renewed node, and a new rotation can
	// start after a cancelled one.
	if err := mgr.StartRotation(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.FinishRotation(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	if mgr.Trust().Rotating {
		t.Fatal("forced finish left the rotation open")
	}
}

func TestHeartbeat_recordsServedSerialAndRenewalSupport(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	issued := "beef01"
	id := seedHeartbeatNode(t, pool, &issued)
	svc := NewNodeService(pool)
	na := time.Now().UTC().Add(time.Hour)
	if _, err := svc.UpdateNodeHeartbeat(ctx, id, NodeHeartbeatRequest{
		Status: "online", MTLSReady: true, CertSerial: "beef01", CertNotAfter: &na, CertRenewal: true,
	}); err != nil {
		t.Fatal(err)
	}
	var served *string
	var renewal bool
	if err := pool.QueryRow(ctx, `SELECT served_cert_serial, cert_renewal_supported FROM nodes WHERE id = $1`, id).
		Scan(&served, &renewal); err != nil {
		t.Fatal(err)
	}
	if served == nil || *served != "beef01" || !renewal {
		t.Fatalf("served=%v renewal=%v", served, renewal)
	}
	// A stale advert (old serial right after a renewal) does not overwrite it.
	if _, err := pool.Exec(ctx, `UPDATE nodes SET cert_serial = 'beef02' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if rejected, err := svc.UpdateNodeHeartbeat(ctx, id, NodeHeartbeatRequest{
		Status: "online", MTLSReady: true, CertSerial: "beef01", CertNotAfter: &na,
	}); err != nil || !rejected {
		t.Fatalf("stale advert: rejected=%v err=%v", rejected, err)
	}
	_ = pool.QueryRow(ctx, `SELECT served_cert_serial, cert_renewal_supported FROM nodes WHERE id = $1`, id).Scan(&served, &renewal)
	if *served != "beef01" || renewal {
		t.Fatalf("after stale advert: served=%v renewal=%v", *served, renewal)
	}
}

func newTestCSR(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}
