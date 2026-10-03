package tlsutil

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/voidrunner3074/aastro/internal/testutil/certgen"
)

func served(t *testing.T, r *Reloader) int64 {
	t.Helper()

	s, err := certgen.ServedSerial(r.ServerConfig())
	require.NoError(t, err)

	return s
}

func TestReloader(t *testing.T) {
	setup := func(t *testing.T) (certFile, keyFile, caFile string, ca *x509.Certificate, caKey *ecdsa.PrivateKey) {
		t.Helper()

		dir := t.TempDir()
		ca, caKey, _ = certgen.NewCA()
		certFile = filepath.Join(dir, "tls.crt")
		keyFile = filepath.Join(dir, "tls.key")
		caFile = filepath.Join(dir, "ca.crt")

		return certFile, keyFile, caFile, ca, caKey
	}

	newReloader := func(t *testing.T, certFile, keyFile string) *Reloader {
		t.Helper()

		r, err := NewReloader(ReloaderConfig{
			CertFile: certFile, KeyFile: keyFile, MinVersion: tls.VersionTLS12,
		})
		require.NoError(t, err)

		return r
	}

	t.Run("serves the new serial after rotation, without restart", func(t *testing.T) {
		certFile, keyFile, _, ca, caKey := setup(t)

		cp, kp := certgen.NewLeaf(ca, caKey, 1001)
		certgen.WriteAtomic(certFile, cp)
		certgen.WriteAtomic(keyFile, kp)

		r := newReloader(t, certFile, keyFile)
		assert.Equal(t, int64(1001), served(t, r))

		cp2, kp2 := certgen.NewLeaf(ca, caKey, 2002)
		certgen.WriteAtomic(keyFile, kp2)
		certgen.WriteAtomic(certFile, cp2)
		require.NoError(t, r.Load())

		assert.Equal(t, int64(2002), served(t, r))
	})

	t.Run("keeps the old cert when the new one on disk is malformed", func(t *testing.T) {
		certFile, keyFile, _, ca, caKey := setup(t)

		cp, kp := certgen.NewLeaf(ca, caKey, 1001)
		certgen.WriteAtomic(certFile, cp)
		certgen.WriteAtomic(keyFile, kp)
		r := newReloader(t, certFile, keyFile)

		certgen.WriteAtomic(certFile, []byte("not a certificate"))
		require.Error(t, r.Load())
		assert.Equal(t, int64(1001), served(t, r))
	})

	t.Run("does not swap anything when only the CA fails (partial failure)", func(t *testing.T) {
		certFile, keyFile, caFile, ca, caKey := setup(t)

		cp, kp := certgen.NewLeaf(ca, caKey, 1001)
		certgen.WriteAtomic(certFile, cp)
		certgen.WriteAtomic(keyFile, kp)
		certgen.WriteAtomic(caFile, mustCAPEM(ca))

		r, err := NewReloader(ReloaderConfig{
			CertFile: certFile, KeyFile: keyFile, CAFile: caFile,
			MinVersion: tls.VersionTLS12,
		})
		require.NoError(t, err)

		cp2, kp2 := certgen.NewLeaf(ca, caKey, 2002)
		certgen.WriteAtomic(certFile, cp2)
		certgen.WriteAtomic(keyFile, kp2)
		certgen.WriteAtomic(caFile, []byte("garbage"))

		require.Error(t, r.Load())
		assert.Equal(t, int64(1001), served(t, r))
	})

	t.Run("rotates client-CA trust dynamically (GetConfigForClient)", func(t *testing.T) {
		certFile, keyFile, caFile, ca, caKey := setup(t)

		sc, sk := certgen.NewLeaf(ca, caKey, 9000)
		certgen.WriteAtomic(certFile, sc)
		certgen.WriteAtomic(keyFile, sk)

		ca1, ca1Key, ca1PEM := certgen.NewCA()
		ca2, ca2Key, ca2PEM := certgen.NewCA()
		certgen.WriteAtomic(caFile, ca1PEM)

		r, err := NewReloader(ReloaderConfig{
			CertFile: certFile, KeyFile: keyFile, CAFile: caFile,
			MinVersion: tls.VersionTLS12,
			ClientAuth: tls.RequireAndVerifyClientCert,
		})
		require.NoError(t, err)

		addr, stop := certgen.StartTLSServer(r.ServerConfig())
		t.Cleanup(stop)

		c1 := certgen.KeyPair(certgen.NewLeaf(ca1, ca1Key, 1))
		c2 := certgen.KeyPair(certgen.NewLeaf(ca2, ca2Key, 2))

		require.NoError(t, certgen.MTLSDial(addr, c1)) // CA1 trusted
		require.Error(t, certgen.MTLSDial(addr, c2))   // CA2 not yet

		certgen.WriteAtomic(caFile, ca2PEM) // rotate trust to CA2
		require.NoError(t, r.Load())

		require.Error(t, certgen.MTLSDial(addr, c1))   // CA1 no longer
		require.NoError(t, certgen.MTLSDial(addr, c2)) // CA2 now
	})
}

func TestReloaderALPN(t *testing.T) {
	dir := t.TempDir()
	ca, caKey, caPEM := certgen.NewCA()
	cp, kp := certgen.NewLeaf(ca, caKey, 1)

	certFile, keyFile, caFile := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"), filepath.Join(dir, "ca.crt")
	certgen.WriteAtomic(certFile, cp)
	certgen.WriteAtomic(keyFile, kp)
	certgen.WriteAtomic(caFile, caPEM)

	for _, tt := range []struct {
		name         string
		disableHTTP2 bool
		want         []string
	}{
		{name: "h2 and http/1.1 by default", want: []string{"h2", "http/1.1"}},
		{name: "http/1.1 only with DisableHTTP2", disableHTTP2: true, want: []string{"http/1.1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewReloader(ReloaderConfig{
				CertFile: certFile, KeyFile: keyFile, CAFile: caFile,
				MinVersion:   tls.VersionTLS12,
				ClientAuth:   tls.RequireAndVerifyClientCert,
				DisableHTTP2: tt.disableHTTP2,
			})
			require.NoError(t, err)

			srv := r.ServerConfig()
			assert.Equal(t, tt.want, srv.NextProtos, "server")
			assert.Equal(t, tt.want, r.ClientConfig().NextProtos, "client")

			// With client auth, the handshake runs on the per-client config.
			perClient, err := srv.GetConfigForClient(&tls.ClientHelloInfo{})
			require.NoError(t, err)
			assert.Equal(t, tt.want, perClient.NextProtos, "server, per-client config")
		})
	}
}

func mustCAPEM(_ *x509.Certificate) []byte {
	_, _, pem := certgen.NewCA()
	return pem
}
