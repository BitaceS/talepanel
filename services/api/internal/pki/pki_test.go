package pki

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"net"
	"testing"
	"time"
)

func newCSR(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: cn},
		DNSNames: []string{"evil.example.com"},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func parseCert(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	b, _ := pem.Decode(certPEM)
	if b == nil {
		t.Fatal("no PEM block")
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func pool(t *testing.T, ca *CA) *x509.CertPool {
	t.Helper()
	p := x509.NewCertPool()
	if !p.AppendCertsFromPEM(ca.CertPEM) {
		t.Fatal("bad CA pem")
	}
	return p
}

func TestGenerateCA_isCAAndParses(t *testing.T) {
	ca, err := GenerateCA()
	if err != nil {
		t.Fatal(err)
	}
	c := parseCert(t, ca.CertPEM)
	if !c.IsCA || !c.BasicConstraintsValid {
		t.Fatal("not a CA")
	}
	if c.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Fatal("missing CertSign")
	}
	if c.NotBefore.After(time.Now()) {
		t.Fatal("not_before in the future")
	}
	if c.NotAfter.Before(time.Now().Add(9 * 365 * 24 * time.Hour)) {
		t.Fatal("CA validity too short")
	}
	if b, _ := pem.Decode(ca.KeyPEM); b == nil {
		t.Fatal("bad key pem")
	}
}

func TestSignCSR_forcesCNAndSAN(t *testing.T) {
	ca, _ := GenerateCA()
	const cn = "11111111-2222-3333-4444-555555555555"
	certPEM, serial, notAfter, err := ca.SignCSR(newCSR(t, "evil"), cn, []string{"daemon"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c := parseCert(t, certPEM)
	if c.Subject.CommonName != cn {
		t.Fatalf("CN = %q", c.Subject.CommonName)
	}
	if len(c.DNSNames) != 1 || c.DNSNames[0] != "daemon" {
		t.Fatalf("SANs = %v", c.DNSNames)
	}
	if serial == "" || !notAfter.Equal(c.NotAfter) {
		t.Fatal("bad serial/notAfter")
	}
	for _, usage := range []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth} {
		if _, err := c.Verify(x509.VerifyOptions{Roots: pool(t, ca), KeyUsages: []x509.ExtKeyUsage{usage}, DNSName: "daemon"}); err != nil {
			t.Fatalf("verify usage %v: %v", usage, err)
		}
	}
}

func TestSignCSR_validityBackdated(t *testing.T) {
	ca, _ := GenerateCA()
	certPEM, _, _, err := ca.SignCSR(newCSR(t, "x"), "cn", nil, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c := parseCert(t, certPEM)
	if !c.NotBefore.Before(time.Now()) {
		t.Fatal("not backdated")
	}
	if d := time.Until(c.NotAfter) - 24*time.Hour; d > time.Minute || d < -time.Minute {
		t.Fatalf("NotAfter off by %v", d)
	}
}

func TestSignCSR_rejectsBadCSR(t *testing.T) {
	ca, _ := GenerateCA()
	if _, _, _, err := ca.SignCSR([]byte("junk"), "cn", nil, time.Hour); err == nil {
		t.Fatal("expected error")
	}
}

func csrWithKey(t *testing.T, key any) []byte {
	t.Helper()
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "x"},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func TestSignCSR_onlyECDSAP256Keys(t *testing.T) {
	ca, err := GenerateCA()
	if err != nil {
		t.Fatal(err)
	}
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, edKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for name, key := range map[string]any{"P-384": p384, "Ed25519": edKey, "RSA": rsaKey} {
		if _, _, _, err := ca.SignCSR(csrWithKey(t, key), "cn", nil, time.Hour); err == nil {
			t.Fatalf("%s CSR key accepted", name)
		}
	}
	p256, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ca.SignCSR(csrWithKey(t, p256), "cn", nil, time.Hour); err != nil {
		t.Fatalf("P-256 CSR rejected: %v", err)
	}
}

func TestIssueClientCert_chainsToCA(t *testing.T) {
	ca, _ := GenerateCA()
	certPEM, keyPEM, err := ca.IssueClientCert("panel")
	if err != nil {
		t.Fatal(err)
	}
	c := parseCert(t, certPEM)
	if _, err := c.Verify(x509.VerifyOptions{Roots: pool(t, ca), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatal(err)
	}
	if b, _ := pem.Decode(keyPEM); b == nil {
		t.Fatal("bad key pem")
	}
}

func TestSignCSR_ipSANGoesToIPAddresses(t *testing.T) {
	ca, _ := GenerateCA()
	certPEM, _, _, err := ca.SignCSR(newCSR(t, "x"), "cn", []string{"10.1.2.3", "node.example.com", "2001:db8::1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c := parseCert(t, certPEM)
	if len(c.DNSNames) != 1 || c.DNSNames[0] != "node.example.com" {
		t.Fatalf("DNSNames = %v", c.DNSNames)
	}
	if len(c.IPAddresses) != 2 || !c.IPAddresses[0].Equal(net.ParseIP("10.1.2.3")) || !c.IPAddresses[1].Equal(net.ParseIP("2001:db8::1")) {
		t.Fatalf("IPAddresses = %v", c.IPAddresses)
	}
	if _, err := c.Verify(x509.VerifyOptions{Roots: pool(t, ca), DNSName: "10.1.2.3"}); err != nil {
		t.Fatalf("verify by IP: %v", err)
	}
}

func TestSignCSR_rejectsBadArgs(t *testing.T) {
	ca, _ := GenerateCA()
	csr := newCSR(t, "x")
	if _, _, _, err := ca.SignCSR(csr, "cn", nil, 0); err == nil {
		t.Fatal("expected error for zero validity")
	}
	if _, _, _, err := ca.SignCSR(csr, "cn", nil, -time.Hour); err == nil {
		t.Fatal("expected error for negative validity")
	}
	if _, _, _, err := ca.SignCSR(csr, "", nil, time.Hour); err == nil {
		t.Fatal("expected error for empty cn")
	}
}
