package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"

	"github.com/starwalkn/aastro"
	"github.com/starwalkn/aastro/internal/logger"
	"github.com/starwalkn/aastro/internal/server"
	"github.com/starwalkn/aastro/internal/tlsutil"
)

const (
	shutdownTimeout  = 10 * time.Second
	bootstrapTimeout = 30 * time.Second
)

func runGateway(cfgPath string) int {
	cfg, err := aastro.LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aastro: %v\n", err)
		return 2 //nolint:mnd // exit code
	}

	log, err := logger.New(cfg.Debug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aastro: logger init: %v\n", err)
		return 1
	}
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bootstrapCtx, cancelBootstrap := context.WithTimeout(ctx, bootstrapTimeout)
	srv, err := server.New(bootstrapCtx, cfg.Gateway, version, log)
	cancelBootstrap()
	if err != nil {
		log.Error("server init failed", zap.Error(err))
		return 1
	}

	var (
		bundle      = srv.GetBundle()
		dirs        []string
		watcherDone chan struct{}
	)

	if bundle.TLSRegistry != nil {
		dirs = bundle.TLSRegistry.Dirs()
	}

	if len(dirs) > 0 {
		watcher, watcherErr := fsnotify.NewWatcher()
		if watcherErr != nil {
			log.Error("fsnotify init failed", zap.Error(watcherErr))
			return 1
		}

		for _, dir := range dirs {
			if addErr := watcher.Add(dir); addErr != nil {
				log.Error(
					"cannot add tls dir to watcher",
					zap.String("dir", dir),
					zap.Error(addErr),
				)

				_ = watcher.Close()

				return 1
			}
		}

		watcherDone = make(chan struct{})

		cw := newCertWatcher(watcher, bundle.TLSRegistry, log)
		go func() {
			defer close(watcherDone)
			cw.run(ctx)
		}()
	}

	serverErrCh := make(chan error, 1)
	go func() {
		if startErr := srv.Start(); startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
			serverErrCh <- startErr
			stop()
			return
		}
		serverErrCh <- nil
	}()

	log.Info("server started")

	<-ctx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err = srv.Stop(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", zap.Error(err))
	}

	srvErr := <-serverErrCh

	if watcherDone != nil {
		<-watcherDone
	}

	if srvErr != nil {
		log.Error("server error", zap.Error(srvErr))
		return 1
	}

	log.Info("server stopped")

	return 0
}

const (
	debounce  = 200 * time.Millisecond
	reloadOps = fsnotify.Create | fsnotify.Write | fsnotify.Rename
)

type certWatcher struct {
	watcher  *fsnotify.Watcher
	reg      *tlsutil.Registry
	log      *zap.Logger
	debounce time.Duration
	timers   map[string]*time.Timer
}

func newCertWatcher(w *fsnotify.Watcher, reg *tlsutil.Registry, log *zap.Logger) *certWatcher {
	return &certWatcher{
		watcher:  w,
		reg:      reg,
		log:      log,
		debounce: debounce,
		timers:   make(map[string]*time.Timer),
	}
}

func (cw *certWatcher) run(ctx context.Context) {
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

func (cw *certWatcher) handleEvent(ctx context.Context, ev fsnotify.Event) {
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

func (cw *certWatcher) reload(ctx context.Context, dir string) {
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

func (cw *certWatcher) stop() {
	for _, t := range cw.timers {
		t.Stop()
	}

	if err := cw.watcher.Close(); err != nil {
		cw.log.Warn("cannot close tls watcher", zap.Error(err))
	}

	cw.log.Info("tls watcher stopped")
}
