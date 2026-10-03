package tlsutil

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/voidrunner3074/aastro/internal/testutil/certgen"
)

func TestRegistry(t *testing.T) {
	setup := func(t *testing.T) (dir string, ca *x509.Certificate, caKey *ecdsa.PrivateKey) {
		t.Helper()

		dir = t.TempDir()
		ca, caKey, _ = certgen.NewCA()

		return dir, ca, caKey
	}

	reloaderIn := func(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, d, name string, serial int64) (*Reloader, string, string) {
		t.Helper()

		require.NoError(t, os.MkdirAll(d, 0o755))

		cf := filepath.Join(d, name+".crt")
		kf := filepath.Join(d, name+".key")

		cp, kp := certgen.NewLeaf(ca, caKey, serial)
		certgen.WriteAtomic(cf, cp)
		certgen.WriteAtomic(kf, kp)

		r, err := NewReloader(ReloaderConfig{
			CertFile: cf, KeyFile: kf, MinVersion: tls.VersionTLS12,
		})
		require.NoError(t, err)

		return r, cf, kf
	}

	t.Run("deduplicates a directory shared by multiple reloaders", func(t *testing.T) {
		dir, ca, caKey := setup(t)

		shared := filepath.Join(dir, "shared")
		r1, _, _ := reloaderIn(t, ca, caKey, shared, "a", 1)
		r2, _, _ := reloaderIn(t, ca, caKey, shared, "b", 2)

		reg := NewRegistry()
		reg.Register(r1)
		reg.Register(r2)

		assert.Len(t, reg.Dirs(), 1)
	})

	t.Run("reloads healthy reloaders under a dir and reports the broken one", func(t *testing.T) {
		dir, ca, caKey := setup(t)

		shared := filepath.Join(dir, "shared")
		rOK, okCert, okKey := reloaderIn(t, ca, caKey, shared, "ok", 100)
		rBad, badCert, _ := reloaderIn(t, ca, caKey, shared, "bad", 200)

		reg := NewRegistry()
		reg.Register(rOK)
		reg.Register(rBad)

		cp, kp := certgen.NewLeaf(ca, caKey, 101)
		certgen.WriteAtomic(okKey, kp)
		certgen.WriteAtomic(okCert, cp)
		certgen.WriteAtomic(badCert, []byte("garbage"))

		errs := reg.ReloadDir(shared)

		assert.Len(t, errs, 1)
		assert.Equal(t, int64(101), served(t, rOK))
	})
}
