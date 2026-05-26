package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/starwalkn/aastro"
	"github.com/starwalkn/aastro/internal/logger"
	"github.com/starwalkn/aastro/internal/server"
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bootstrapCtx, cancelBootstrap := context.WithTimeout(ctx, bootstrapTimeout)
	srv, err := server.New(bootstrapCtx, cfg.Gateway, version, log)
	cancelBootstrap()
	if err != nil {
		log.Error("server init failed", zap.Error(err))
		return 1
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

	if err = <-serverErrCh; err != nil {
		log.Error("server error", zap.Error(err))
		return 1
	}

	log.Info("server stopped")
	return 0
}
