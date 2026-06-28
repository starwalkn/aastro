package certwatcher_test

import (
	"context"
	"crypto/tls"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/starwalkn/aastro/internal/certwatcher"
	"github.com/starwalkn/aastro/internal/testutil/certgen"
	"github.com/starwalkn/aastro/internal/tlsutil"
)

type countingRegistry struct {
	mu    sync.Mutex
	dirs  []string
	calls map[string]int
}

func (r *countingRegistry) Dirs() []string { return r.dirs }

func (r *countingRegistry) ReloadDir(dir string) []error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls[dir]++

	return nil
}

func (r *countingRegistry) count(dir string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.calls[dir]
}

var _ = Describe("Watcher", func() {
	It("reloads after a rotation in a watched directory", func() {
		dir := GinkgoT().TempDir()
		certFile := filepath.Join(dir, "tls.crt")
		keyFile := filepath.Join(dir, "tls.key")

		ca, caKey, _ := certgen.NewCA()
		cp, kp := certgen.NewLeaf(ca, caKey, 1001)
		certgen.WriteAtomic(certFile, cp)
		certgen.WriteAtomic(keyFile, kp)

		r, err := tlsutil.NewReloader(tlsutil.ReloaderConfig{
			CertFile: certFile, KeyFile: keyFile, MinVersion: tls.VersionTLS12,
		})
		Expect(err).NotTo(HaveOccurred())

		reg := tlsutil.NewRegistry()
		reg.Register(r)

		w, err := fsnotify.NewWatcher()
		Expect(err).NotTo(HaveOccurred())
		for _, d := range reg.Dirs() {
			Expect(w.Add(d)).To(Succeed())
		}

		cw := certwatcher.New(w, reg, zap.NewNop(),
			certwatcher.WithDebounce(20*time.Millisecond))

		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)
		go cw.Run(ctx)

		cp2, kp2 := certgen.NewLeaf(ca, caKey, 2002)
		certgen.WriteAtomic(keyFile, kp2)
		certgen.WriteAtomic(certFile, cp2)

		Eventually(func() int64 {
			s, _ := certgen.ServedSerial(r.ServerConfig())
			return s
		}, 2*time.Second, 20*time.Millisecond).Should(Equal(int64(2002)))
	})

	It("coalesces a burst of events into far fewer reloads", func() {
		dir := GinkgoT().TempDir()
		target := filepath.Join(dir, "tls.crt")
		certgen.WriteAtomic(target, []byte("x"))

		fake := &countingRegistry{dirs: []string{dir}, calls: map[string]int{}}

		w, err := fsnotify.NewWatcher()
		Expect(err).NotTo(HaveOccurred())
		Expect(w.Add(dir)).To(Succeed())

		cw := certwatcher.New(w, fake, zap.NewNop(),
			certwatcher.WithDebounce(120*time.Millisecond))

		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)
		go cw.Run(ctx)

		const burst = 10
		for i := range burst {
			certgen.WriteAtomic(target, []byte{byte(i)})
		}

		Eventually(func() int { return fake.count(dir) }, time.Second).
			Should(BeNumerically(">=", 1))
		Consistently(func() int { return fake.count(dir) }, 300*time.Millisecond).
			Should(BeNumerically("<=", 2))
	})

	It("stops cleanly when the context is cancelled", func() {
		dir := GinkgoT().TempDir()

		w, err := fsnotify.NewWatcher()
		Expect(err).NotTo(HaveOccurred())
		Expect(w.Add(dir)).To(Succeed())

		cw := certwatcher.New(w,
			&countingRegistry{dirs: []string{dir}, calls: map[string]int{}},
			zap.NewNop())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { cw.Run(ctx); close(done) }()

		cancel()
		Eventually(done).Should(BeClosed())
	})
})
