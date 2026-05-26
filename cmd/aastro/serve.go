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

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/starwalkn/aastro"
	"github.com/starwalkn/aastro/internal/logger"
	"github.com/starwalkn/aastro/internal/server"
)

const (
	shutdownTimeout  = 10 * time.Second
	bootstrapTimeout = 30 * time.Second
)

// version is populated via -ldflags "-X main.version=…" at build time.
// Empty when built without ldflags; tracing then omits service.version.
var version string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run HTTP server",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runServe()
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runServe() error {
	if cfgPath == "" {
		cfgPath = os.Getenv("AASTRO_CONFIG")
	}
	if cfgPath == "" {
		cfgPath = fallbackConfigPath
	}

	cfg, err := aastro.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	log, err := logger.New(cfg.Debug)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bootstrapCtx, cancelBootstrap := context.WithTimeout(ctx, bootstrapTimeout)

	srv, err := server.New(bootstrapCtx, cfg.Gateway, version, log)
	cancelBootstrap()
	if err != nil {
		return fmt.Errorf("server init: %w", err)
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

	// Drain Start's exit so we don't lose a listener error on the floor
	if err = <-serverErrCh; err != nil {
		log.Error("server error", zap.Error(err))
	}

	log.Info("server stopped")

	return nil
}
