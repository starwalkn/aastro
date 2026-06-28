package tlsutil

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/starwalkn/aastro/internal/testutil/certgen"
)

func served(r *Reloader) int64 {
	GinkgoHelper()

	s, err := certgen.ServedSerial(r.ServerConfig())
	Expect(err).NotTo(HaveOccurred())

	return s
}

var _ = Describe("Reloader", func() {
	var (
		dir               string
		certFile, keyFile string
		caFile            string
		ca                *x509.Certificate
		caKey             *ecdsa.PrivateKey
	)

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		ca, caKey, _ = certgen.NewCA()
		certFile = filepath.Join(dir, "tls.crt")
		keyFile = filepath.Join(dir, "tls.key")
		caFile = filepath.Join(dir, "ca.crt")
	})

	newReloader := func() *Reloader {
		r, err := NewReloader(ReloaderConfig{
			CertFile: certFile, KeyFile: keyFile, MinVersion: tls.VersionTLS12,
		})
		Expect(err).NotTo(HaveOccurred())
		return r
	}

	It("serves the new serial after rotation, without restart", func() {
		cp, kp := certgen.NewLeaf(ca, caKey, 1001)
		certgen.WriteAtomic(certFile, cp)
		certgen.WriteAtomic(keyFile, kp)

		r := newReloader()
		Expect(served(r)).To(Equal(int64(1001)))

		cp2, kp2 := certgen.NewLeaf(ca, caKey, 2002)
		certgen.WriteAtomic(keyFile, kp2)
		certgen.WriteAtomic(certFile, cp2)
		Expect(r.Load()).To(Succeed())

		Expect(served(r)).To(Equal(int64(2002)))
	})

	It("keeps the old cert when the new one on disk is malformed", func() {
		cp, kp := certgen.NewLeaf(ca, caKey, 1001)
		certgen.WriteAtomic(certFile, cp)
		certgen.WriteAtomic(keyFile, kp)
		r := newReloader()

		certgen.WriteAtomic(certFile, []byte("not a certificate"))
		Expect(r.Load()).To(HaveOccurred())
		Expect(served(r)).To(Equal(int64(1001)))
	})

	It("does not swap anything when only the CA fails (partial failure)", func() {
		cp, kp := certgen.NewLeaf(ca, caKey, 1001)
		certgen.WriteAtomic(certFile, cp)
		certgen.WriteAtomic(keyFile, kp)
		certgen.WriteAtomic(caFile, mustCAPEM(ca))

		r, err := NewReloader(ReloaderConfig{
			CertFile: certFile, KeyFile: keyFile, CAFile: caFile,
			MinVersion: tls.VersionTLS12,
		})
		Expect(err).NotTo(HaveOccurred())

		cp2, kp2 := certgen.NewLeaf(ca, caKey, 2002)
		certgen.WriteAtomic(certFile, cp2)
		certgen.WriteAtomic(keyFile, kp2)
		certgen.WriteAtomic(caFile, []byte("garbage"))

		Expect(r.Load()).To(HaveOccurred())
		Expect(served(r)).To(Equal(int64(1001)))
	})

	It("rotates client-CA trust dynamically (GetConfigForClient)", func() {
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
		Expect(err).NotTo(HaveOccurred())

		addr, stop := certgen.StartTLSServer(r.ServerConfig())
		DeferCleanup(stop)

		c1 := certgen.KeyPair(certgen.NewLeaf(ca1, ca1Key, 1))
		c2 := certgen.KeyPair(certgen.NewLeaf(ca2, ca2Key, 2))

		Expect(certgen.MTLSDial(addr, c1)).To(Succeed())    // CA1 trusted
		Expect(certgen.MTLSDial(addr, c2)).NotTo(Succeed()) // CA2 not yet

		certgen.WriteAtomic(caFile, ca2PEM) // rotate trust to CA2
		Expect(r.Load()).To(Succeed())

		Expect(certgen.MTLSDial(addr, c1)).NotTo(Succeed()) // CA1 no longer
		Expect(certgen.MTLSDial(addr, c2)).To(Succeed())    // CA2 now
	})
})

func mustCAPEM(_ *x509.Certificate) []byte {
	_, _, pem := certgen.NewCA()
	return pem
}
