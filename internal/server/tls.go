package server

import (
	"crypto/tls"
	"fmt"

	"github.com/starwalkn/aastro"
	"github.com/starwalkn/aastro/internal/tlsutil"
)

func buildTLSConfig(cfg aastro.ServerTLSConfig) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil //nolint:nilnil // its ok here
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load server keypair: %w", err)
	}

	minVer, err := tlsutil.ParseVersion(cfg.MinVersion)
	if err != nil {
		return nil, err
	}

	clientAuth, err := tlsutil.ParseClientAuth(cfg.ClientAuth)
	if err != nil {
		return nil, err
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   minVer, // #nosec G402
		ClientAuth:   clientAuth,
		NextProtos:   []string{"h2", "http/1.1"},
	}

	if clientAuth != tls.NoClientCert {
		pool, caErr := tlsutil.LoadCAPool(cfg.ClientCAFile)
		if caErr != nil {
			return nil, fmt.Errorf("load client CA: %w", caErr)
		}

		tlsCfg.ClientCAs = pool
	}

	return tlsCfg, nil
}
