package tlsutil

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/starwalkn/aastro/internal/testutil/certgen"
)

var _ = Describe("Registry", func() {
	var (
		dir   string
		ca    *x509.Certificate
		caKey *ecdsa.PrivateKey
	)

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		ca, caKey, _ = certgen.NewCA()
	})

	reloaderIn := func(d, name string, serial int64) (*Reloader, string, string) {
		Expect(os.MkdirAll(d, 0o755)).To(Succeed())

		cf := filepath.Join(d, name+".crt")
		kf := filepath.Join(d, name+".key")

		cp, kp := certgen.NewLeaf(ca, caKey, serial)
		certgen.WriteAtomic(cf, cp)
		certgen.WriteAtomic(kf, kp)

		r, err := NewReloader(ReloaderConfig{
			CertFile: cf, KeyFile: kf, MinVersion: tls.VersionTLS12,
		})

		Expect(err).NotTo(HaveOccurred())

		return r, cf, kf
	}

	It("deduplicates a directory shared by multiple reloaders", func() {
		shared := filepath.Join(dir, "shared")
		r1, _, _ := reloaderIn(shared, "a", 1)
		r2, _, _ := reloaderIn(shared, "b", 2)

		reg := NewRegistry()
		reg.Register(r1)
		reg.Register(r2)

		Expect(reg.Dirs()).To(HaveLen(1))
	})

	It("reloads healthy reloaders under a dir and reports the broken one", func() {
		shared := filepath.Join(dir, "shared")
		rOK, okCert, okKey := reloaderIn(shared, "ok", 100)
		rBad, badCert, _ := reloaderIn(shared, "bad", 200)

		reg := NewRegistry()
		reg.Register(rOK)
		reg.Register(rBad)

		cp, kp := certgen.NewLeaf(ca, caKey, 101)
		certgen.WriteAtomic(okKey, kp)
		certgen.WriteAtomic(okCert, cp)
		certgen.WriteAtomic(badCert, []byte("garbage"))

		errs := reg.ReloadDir(shared)

		Expect(errs).To(HaveLen(1))
		Expect(served(rOK)).To(Equal(int64(101)))
	})
})
