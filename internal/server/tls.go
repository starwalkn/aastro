package server

import (
	"crypto/tls"
	"errors"

	"github.com/voidrunner3074/aastro"
	"github.com/voidrunner3074/aastro/internal/tlsutil"
)

func buildTLSConfig(cfg aastro.ServerTLSConfig, http2 string, reg *tlsutil.Registry) (*tls.Config, error) {
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
		CertFile:     cfg.CertFile,
		CAFile:       cfg.ClientCAFile,
		KeyFile:      cfg.KeyFile,
		MinVersion:   minVer,
		ClientAuth:   clientAuth,
		DisableHTTP2: http2 == "off",
	})
	if err != nil {
		return nil, err
	}

	reg.Register(r)

	return r.ServerConfig(), nil
}
