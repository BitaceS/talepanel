// Package pki implements the panel's private certificate authority used for
// mutual TLS between the panel and its daemons. The CA signs daemon CSRs and
// issues the panel's own client certificate.
package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"
)

const (
	caValidity = 10 * 365 * 24 * time.Hour
	// leafValidity is the lifetime of the panel's own client certificate.
	leafValidity = 2 * 365 * 24 * time.Hour
	// clockSkew backdates not_before so slightly-behind clocks accept the cert.
	clockSkew = 5 * time.Minute
)

// CA is a PEM-encoded CA certificate and its PKCS#8 private key.
type CA struct {
	CertPEM []byte
	KeyPEM  []byte
}

func randomSerial() (*big.Int, error) {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating serial: %w", err)
	}
	return n, nil
}

func encodeKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshaling key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// GenerateCA creates a self-signed ECDSA P-256 CA valid for ~10 years.
func GenerateCA() (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "TalePanel Root CA", Organization: []string{"TalePanel"}},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("creating CA certificate: %w", err)
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return nil, err
	}
	return &CA{
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:  keyPEM,
	}, nil
}

// ParseCSR decodes and signature-checks a PEM PKCS#10 certificate request.
func ParseCSR(pemBytes []byte) (*x509.CertificateRequest, error) {
	b, _ := pem.Decode(pemBytes)
	if b == nil || b.Type != "CERTIFICATE REQUEST" {
		return nil, errors.New("no CERTIFICATE REQUEST PEM block")
	}
	csr, err := x509.ParseCertificateRequest(b.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("CSR signature invalid: %w", err)
	}
	return csr, nil
}

func (ca *CA) parse() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cb, _ := pem.Decode(ca.CertPEM)
	if cb == nil {
		return nil, nil, errors.New("invalid CA certificate PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing CA certificate: %w", err)
	}
	kb, _ := pem.Decode(ca.KeyPEM)
	if kb == nil {
		return nil, nil, errors.New("invalid CA key PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing CA key: %w", err)
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("CA key is not ECDSA")
	}
	return cert, key, nil
}

// sign issues a leaf for pub with the given subject CN and DNS SANs.
func (ca *CA) sign(pub any, cn string, sans []string, validFor time.Duration) ([]byte, string, time.Time, error) {
	caCert, caKey, err := ca.parse()
	if err != nil {
		return nil, "", time.Time{}, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, "", time.Time{}, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		DNSNames:              sans,
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(validFor).UTC().Truncate(time.Second), // DER has 1s resolution
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, pub, caKey)
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf("signing certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), serial.Text(16), tmpl.NotAfter, nil
}

// SignCSR signs a PKCS#10 CSR. The subject CN and SANs come from the caller,
// never from the CSR, so a daemon cannot claim another node's identity. Only
// the CSR's public key is used. The serial is lowercase hex.
func (ca *CA) SignCSR(csrPEM []byte, cn string, sans []string, validFor time.Duration) ([]byte, string, time.Time, error) {
	csr, err := ParseCSR(csrPEM)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	return ca.sign(csr.PublicKey, cn, sans, validFor)
}

// IssueClientCert generates a fresh keypair and a certificate with subject CN
// cn, for the panel's own use when calling daemons.
func (ca *CA) IssueClientCert(cn string) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generating key: %w", err)
	}
	certPEM, _, _, err := ca.sign(&key.PublicKey, cn, nil, leafValidity)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return nil, nil, err
	}
	return certPEM, keyPEM, nil
}
