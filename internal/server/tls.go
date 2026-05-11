package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/starwalkn/kono"
)

func buildTLSConfig(cfg kono.ServerTLSConfig) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load server keypair: %w", err)
	}

	minVer, err := parseTLSVersion(cfg.MinVersion)
	if err != nil {
		return nil, err
	}

	clientAuth, err := parseClientAuth(cfg.ClientAuth)
	if err != nil {
		return nil, err
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   minVer,
		ClientAuth:   clientAuth,
		NextProtos:   []string{"h2", "http/1.1"},
	}

	if clientAuth != tls.NoClientCert {
		pool, err := loadCAPool(cfg.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("load client CA: %w", err)
		}

		tlsCfg.ClientCAs = pool
	}

	return tlsCfg, nil
}

func parseTLSVersion(s string) (uint16, error) {
	switch s {
	case "", "1.2":
		return tls.VersionTLS12, nil
	case "1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported tls min_version: %q (allowed: 1.2, 1.3)", s)
	}
}

func parseClientAuth(s string) (tls.ClientAuthType, error) {
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

func loadCAPool(path string) (*x509.CertPool, error) {
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
