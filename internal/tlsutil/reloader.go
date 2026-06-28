package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
)

type ReloaderConfig struct {
	CertFile, CAFile, KeyFile string
	ServerName                string // client-only
	MinVersion                uint16
	InsecureSkipVerify        bool               // client-only
	ClientAuth                tls.ClientAuthType // server-only
}

type Reloader struct {
	cfg ReloaderConfig

	cert   atomic.Pointer[tls.Certificate]
	caPool atomic.Pointer[x509.CertPool]
}

func NewReloader(cfg ReloaderConfig) (*Reloader, error) {
	r := &Reloader{cfg: cfg}

	if err := r.Load(); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Reloader) Load() error {
	var newCert *tls.Certificate
	var newPool *x509.CertPool

	if r.cfg.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(r.cfg.CertFile, r.cfg.KeyFile)
		if err != nil {
			return fmt.Errorf("load client keypair: %w", err)
		}

		newCert = &cert
	}

	if r.cfg.CAFile != "" {
		pool, err := LoadCAPool(r.cfg.CAFile)
		if err != nil {
			return fmt.Errorf("load CA pool: %w", err)
		}

		newPool = pool
	}

	if newCert != nil {
		r.cert.Store(newCert)
	}

	if newPool != nil {
		r.caPool.Store(newPool)
	}

	return nil
}

func (r *Reloader) ServerConfig() *tls.Config {
	cfg := &tls.Config{
		MinVersion: r.cfg.MinVersion,
		NextProtos: []string{"h2", "http/1.1"},
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return r.cert.Load(), nil
		},
	}

	if r.cfg.ClientAuth == tls.NoClientCert {
		return cfg
	}

	cfg.ClientAuth = r.cfg.ClientAuth
	cfg.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		c := cfg.Clone()
		c.GetConfigForClient = nil
		c.ClientCAs = r.caPool.Load()

		return c, nil
	}

	return cfg
}

func (r *Reloader) ClientConfig() *tls.Config {
	cfg := &tls.Config{
		MinVersion: r.cfg.MinVersion,
		NextProtos: []string{"h2", "http/1.1"},
		ServerName: r.cfg.ServerName,
	}

	if r.cfg.CertFile != "" {
		cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return r.cert.Load(), nil // не nil после успешного NewReloader
		}
	}

	switch {
	case r.cfg.InsecureSkipVerify:
		cfg.InsecureSkipVerify = true // #nosec G402 — явная воля конфига

	case r.cfg.CAFile != "":
		// На клиенте нет per-handshake хука для RootCAs, поэтому гасим
		// дефолтный верификатор и проверяем сами против текущего пула.
		cfg.InsecureSkipVerify = true // #nosec G402 — проверка уехала в VerifyConnection
		cfg.VerifyConnection = r.verifyConnection

		// иначе: системные корни, статическая проверка (они per-process не ротируются)
	}

	return cfg
}

func (r *Reloader) verifyConnection(cs tls.ConnectionState) error {
	if len(cs.PeerCertificates) == 0 {
		return errors.New("tls: no peer certificates")
	}

	opts := x509.VerifyOptions{
		Roots:         r.caPool.Load(),
		DNSName:       cs.ServerName,
		Intermediates: x509.NewCertPool(),
	}

	for _, c := range cs.PeerCertificates[1:] {
		opts.Intermediates.AddCert(c)
	}

	_, err := cs.PeerCertificates[0].Verify(opts)

	return err
}

func (r *Reloader) WatchDirs() []string {
	seen := map[string]struct{}{}
	var dirs []string

	for _, f := range []string{r.cfg.CertFile, r.cfg.KeyFile, r.cfg.CAFile} {
		if f == "" {
			continue
		}

		d := filepath.Dir(f)
		if _, ok := seen[d]; ok {
			continue
		}

		seen[d] = struct{}{}
		dirs = append(dirs, d)
	}

	return dirs
}
