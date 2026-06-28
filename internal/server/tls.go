package server

import (
	"crypto/tls"
	"errors"

	"github.com/starwalkn/aastro"
	"github.com/starwalkn/aastro/internal/tlsutil"
)

func buildTLSConfig(cfg aastro.ServerTLSConfig, reg *tlsutil.Registry) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil //nolint:nilnil // its ok here
	}

	minVer, err := tlsutil.ParseVersion(cfg.MinVersion)
	if err != nil {
		return nil, err
	}

	clientAuth, err := tlsutil.ParseClientAuth(cfg.ClientAuth)
	if err != nil {
		return nil, err
	}

	if clientAuth != tls.NoClientCert && cfg.ClientCAFile == "" {
		return nil, errors.New("client_ca_file required when client_auth verifies certs")
	}

	r, err := tlsutil.NewReloader(tlsutil.ReloaderConfig{
		CertFile:   cfg.CertFile,
		CAFile:     cfg.ClientCAFile,
		KeyFile:    cfg.KeyFile,
		MinVersion: minVer,
		ClientAuth: clientAuth,
	})
	if err != nil {
		return nil, err
	}

	reg.Register(r)

	return r.ServerConfig(), nil
}
