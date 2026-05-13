package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

func ParseVersion(s string) (uint16, error) {
	switch s {
	case "", "1.2":
		return tls.VersionTLS12, nil
	case "1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported tls min_version: %q (allowed: 1.2, 1.3)", s)
	}
}

func ParseClientAuth(s string) (tls.ClientAuthType, error) {
	switch s {
	case "", "none":
		return tls.NoClientCert, nil
	case "optional":
		return tls.VerifyClientCertIfGiven, nil
	case "require":
		return tls.RequireAndVerifyClientCert, nil
	default:
		return 0, fmt.Errorf("unknown client_auth: %q", s)
	}
}

func LoadCAPool(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("no valid certificates found in %q", path)
	}

	return pool, nil
}
