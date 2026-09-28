package hue

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"
)

func TestVerifyBridgeCertificateAcceptsMatchingID(t *testing.T) {
	const id = "ecb5fafffe8848f4"
	raw, roots := mustChain(t, id)
	got, err := verifyBridgeCertificateWithRoots(raw, "ECB5FAFFFE8848F4", roots, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got != id {
		t.Fatalf("id %s", got)
	}
}

func TestVerifyBridgeCertificateRejectsOtherBridge(t *testing.T) {
	raw, roots := mustChain(t, "ecb5fafffe8848f4")
	if _, err := verifyBridgeCertificateWithRoots(raw, "001788fffe000000", roots, time.Now()); err == nil {
		t.Fatal("accepted a certificate for a different bridge")
	}
}

func TestVerifyBridgeCertificateRejectsUnknownCA(t *testing.T) {
	raw, _ := mustChain(t, "ecb5fafffe8848f4")
	roots := x509.NewCertPool()
	if _, err := verifyBridgeCertificateWithRoots(raw, "ecb5fafffe8848f4", roots, time.Now()); err == nil {
		t.Fatal("accepted a certificate that is not signed by the trusted CA")
	}
}

func mustChain(t *testing.T, cn string) ([][]byte, *x509.CertPool) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-hue-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, caKey.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, leafKey.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	return [][]byte{leafDER}, roots
}
