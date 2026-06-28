package certwatcher

import (
	"context"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"

	"github.com/starwalkn/aastro/internal/tlsutil"
)

const (
	defaultDebounce = 200 * time.Millisecond
	reloadOps       = fsnotify.Create | fsnotify.Write | fsnotify.Rename
)

type Option func(*Watcher)

func WithDebounce(d time.Duration) Option {
	return func(w *Watcher) {
		if d > 0 {
			w.debounce = d
		}
	}
}

type Watcher struct {
	watcher  *fsnotify.Watcher
	reg      *tlsutil.Registry
	log      *zap.Logger
	debounce time.Duration
	timers   map[string]*time.Timer
}

func New(w *fsnotify.Watcher, reg *tlsutil.Registry, log *zap.Logger, opts ...Option) *Watcher {
	cw := &Watcher{
		watcher:  w,
		reg:      reg,
		log:      log,
		debounce: defaultDebounce,
		timers:   make(map[string]*time.Timer),
	}

	for _, opt := range opts {
		opt(cw)
	}

	return cw
}

func (cw *Watcher) Run(ctx context.Context) {
	defer cw.stop()

	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-cw.watcher.Events:
			if !ok {
				return
			}

			cw.handleEvent(ctx, ev)

		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}

			cw.log.Warn("watcher sends error event", zap.Error(err))
		}
	}
}

func (cw *Watcher) handleEvent(ctx context.Context, ev fsnotify.Event) {
	if ev.Op&reloadOps == 0 {
		return
	}

	cw.log.Debug(
		"new tls watcher event",
		zap.String("event", ev.Name),
		zap.String("op", ev.Op.String()),
	)

	dir := filepath.Dir(ev.Name)
	if t := cw.timers[dir]; t != nil {
		t.Reset(cw.debounce)
		return
	}

	cw.timers[dir] = time.AfterFunc(cw.debounce, func() {
		cw.reload(ctx, dir)
	})
}

func (cw *Watcher) reload(ctx context.Context, dir string) {
	if ctx.Err() != nil {
		return
	}

	errs := cw.reg.ReloadDir(dir)

	if len(errs) > 0 {
		for _, err := range errs {
			cw.log.Error("tls reload failed, keeping old cert",
				zap.String("dir", dir),
				zap.Error(err),
			)
		}
		return
	}

	cw.log.Info("tls certs reloaded", zap.String("dir", dir))
}

func (cw *Watcher) stop() {
	for _, t := range cw.timers {
		t.Stop()
	}

	if err := cw.watcher.Close(); err != nil {
		cw.log.Warn("cannot close tls watcher", zap.Error(err))
	}

	cw.log.Info("tls watcher stopped")
}
