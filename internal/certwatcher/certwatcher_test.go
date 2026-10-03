package certwatcher_test

import (
	"context"
	"crypto/tls"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/voidrunner3074/aastro/internal/certwatcher"
	"github.com/voidrunner3074/aastro/internal/testutil/certgen"
	"github.com/voidrunner3074/aastro/internal/tlsutil"
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

func TestWatcher(t *testing.T) {
	t.Run("reloads after a rotation in a watched directory", func(t *testing.T) {
		dir := t.TempDir()
		certFile := filepath.Join(dir, "tls.crt")
		keyFile := filepath.Join(dir, "tls.key")

		ca, caKey, _ := certgen.NewCA()
		cp, kp := certgen.NewLeaf(ca, caKey, 1001)
		certgen.WriteAtomic(certFile, cp)
		certgen.WriteAtomic(keyFile, kp)

		r, err := tlsutil.NewReloader(tlsutil.ReloaderConfig{
			CertFile: certFile, KeyFile: keyFile, MinVersion: tls.VersionTLS12,
		})
		require.NoError(t, err)

		reg := tlsutil.NewRegistry()
		reg.Register(r)

		w, err := fsnotify.NewWatcher()
		require.NoError(t, err)
		for _, d := range reg.Dirs() {
			require.NoError(t, w.Add(d))
		}

		cw := certwatcher.New(w, reg, zap.NewNop(),
			certwatcher.WithDebounce(20*time.Millisecond))

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go cw.Run(ctx)

		cp2, kp2 := certgen.NewLeaf(ca, caKey, 2002)
		certgen.WriteAtomic(keyFile, kp2)
		certgen.WriteAtomic(certFile, cp2)

		assert.Eventually(t, func() bool {
			s, _ := certgen.ServedSerial(r.ServerConfig())
			return s == 2002
		}, 2*time.Second, 20*time.Millisecond)
	})

	t.Run("coalesces a burst of events into far fewer reloads", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "tls.crt")
		certgen.WriteAtomic(target, []byte("x"))

		fake := &countingRegistry{dirs: []string{dir}, calls: map[string]int{}}

		w, err := fsnotify.NewWatcher()
		require.NoError(t, err)
		require.NoError(t, w.Add(dir))

		cw := certwatcher.New(w, fake, zap.NewNop(),
			certwatcher.WithDebounce(120*time.Millisecond))

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go cw.Run(ctx)

		const burst = 10
		for i := range burst {
			certgen.WriteAtomic(target, []byte{byte(i)})
		}

		assert.Eventually(t, func() bool { return fake.count(dir) >= 1 }, time.Second, 10*time.Millisecond)
		assert.Never(t, func() bool { return fake.count(dir) > 2 }, 300*time.Millisecond, 10*time.Millisecond)
	})

	t.Run("stops cleanly when the context is cancelled", func(t *testing.T) {
		dir := t.TempDir()

		w, err := fsnotify.NewWatcher()
		require.NoError(t, err)
		require.NoError(t, w.Add(dir))

		cw := certwatcher.New(w,
			&countingRegistry{dirs: []string{dir}, calls: map[string]int{}},
			zap.NewNop())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { cw.Run(ctx); close(done) }()

		cancel()
		assert.Eventually(t, func() bool {
			select {
			case _, ok := <-done:
				return !ok
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond)
	})
}
