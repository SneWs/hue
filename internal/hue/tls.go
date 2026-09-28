package hue

import (
	"crypto/x509"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"
)

//go:embed certs/hue-bridge-ca.pem
var hueBridgeCAPEM []byte

// The embedded PEM is Signify's Hue Bridge CA (CN=root-bridge). Bridge
// certificates are signed by this CA and name the bridge id in the subject.

func hueRootPool() *x509.CertPool {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(hueBridgeCAPEM) {
		panic("hue: embedded Hue Bridge CA is not a PEM certificate")
	}
	return pool
}

// verifyBridgeCertificate checks that raw is a chain signed by the Hue
// Bridge CA and that the leaf subject is the bridge id. expectedID may be
// empty when the id is not known yet; the verified id is then returned.
// Go's default hostname check is not used because these certificates have
// no subject alternative name.
func verifyBridgeCertificate(raw [][]byte, expectedID string) (string, error) {
	return verifyBridgeCertificateWithRoots(raw, expectedID, hueRootPool(), time.Now())
}

func verifyBridgeCertificateWithRoots(raw [][]byte, expectedID string, roots *x509.CertPool, now time.Time) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("bridge presented no certificate")
	}
	certs := make([]*x509.Certificate, 0, len(raw))
	for _, der := range raw {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return "", fmt.Errorf("bridge certificate: %w", err)
		}
		certs = append(certs, cert)
	}
	leaf := certs[0]
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   now,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return "", fmt.Errorf("bridge certificate is not signed by the Hue Bridge CA: %w", err)
	}
	id := bridgeIDFromCert(leaf)
	if !validBridgeID(id) {
		return "", errors.New("bridge certificate subject is not a bridge id")
	}
	if expected := normalizeBridgeID(expectedID); expected != "" && id != expected {
		return "", fmt.Errorf("bridge certificate is for %s, not %s", id, expected)
	}
	return id, nil
}

func bridgeIDFromCert(cert *x509.Certificate) string {
	if id := normalizeBridgeID(cert.Subject.CommonName); validBridgeID(id) {
		return id
	}
	for _, name := range cert.DNSNames {
		if id := normalizeBridgeID(name); validBridgeID(id) {
			return id
		}
	}
	return ""
}

func normalizeBridgeID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

func validBridgeID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}
