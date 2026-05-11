package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/starwalkn/kono"
	"github.com/starwalkn/kono/internal/otelcommon"
)

const adminTimeout = 5 * time.Minute

type Server struct {
	dataServer  *http.Server
	adminServer *http.Server
	router      *kono.Router
	providers   []otelcommon.Provider
	log         *zap.Logger
}

func New(ctx context.Context, cfg kono.GatewayConfig, version string, log *zap.Logger) (*Server, error) {
	bundle, err := bootstrapRouter(ctx, cfg, version, log)
	if err != nil {
		return nil, fmt.Errorf("bootstrap router: %w", err)
	}

	tlsConfig, err := buildTLSConfig(cfg.Server.TLS)
	if err != nil {
		return nil, fmt.Errorf("build server TLS config: %w", err)
	}

	stdLog := zap.NewStdLog(log)

	return &Server{
		dataServer: &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
			Handler:           buildHandler(bundle),
			ReadTimeout:       cfg.Server.Timeout,
			WriteTimeout:      cfg.Server.Timeout,
			ReadHeaderTimeout: cfg.Server.HeaderTimeout,
			TLSConfig:         tlsConfig,
			ErrorLog:          stdLog,
		},
		adminServer: &http.Server{
			Addr:              fmt.Sprintf("%s:%d", cfg.Server.AdminBindAddr, cfg.Server.AdminPort),
			Handler:           buildAdminHandler(bundle, cfg.Server.Pprof.Enabled),
			ReadTimeout:       adminTimeout,
			WriteTimeout:      adminTimeout,
			ReadHeaderTimeout: cfg.Server.HeaderTimeout,
		},
		router:    bundle.Router,
		providers: []otelcommon.Provider{bundle.MeterProvider, bundle.TracerProvider},
		log:       log,
	}, nil
}

func (s *Server) Start() error {
	g := new(errgroup.Group)

	g.Go(func() error {
		err := s.startDataServer()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("data server: %w", err)
	})

	g.Go(func() error {
		err := s.adminServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("admin server: %w", err)
	})

	return g.Wait()
}

func (s *Server) startDataServer() error {
	if s.dataServer.TLSConfig != nil {
		return s.dataServer.ListenAndServeTLS("", "")
	}

	return s.dataServer.ListenAndServe()
}

// Stop drains the HTTP server, closes the router (middleware Closers), then
// flushes observability providers. Order matters: HTTP first so no in-flight
// request writes to a provider that is already shutting down.
func (s *Server) Stop(ctx context.Context) error {
	var errs []error

	if err := s.dataServer.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("data server shutdown: %w", err))
	}

	if err := s.adminServer.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("admin server shutdown: %w", err))
	}

	if err := s.router.Close(); err != nil {
		errs = append(errs, fmt.Errorf("router close: %w", err))
	}

	for _, p := range s.providers {
		if err := p.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("provider shutdown: %w", err))
		}
	}

	return errors.Join(errs...)
}

func bootstrapRouter(ctx context.Context, cfg kono.GatewayConfig, version string, log *zap.Logger) (kono.RouterBundle, error) {
	bundle, err := kono.NewRouter(ctx, kono.RoutingConfigSet{
		Routing:        cfg.Routing,
		Service:        cfg.Service,
		ServiceVersion: version,
		Metrics:        cfg.Server.Metrics,
		Tracing:        cfg.Server.Tracing,
	}, log.Named("router"))
	if err != nil {
		return kono.RouterBundle{}, err
	}

	return bundle, nil
}

func buildHandler(bundle kono.RouterBundle) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/", bundle.Router)

	return mux
}

func buildAdminHandler(bundle kono.RouterBundle, pprofEnabled bool) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /__health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	if bundle.PromRegistry != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(bundle.PromRegistry, promhttp.HandlerOpts{}))
	}

	if pprofEnabled {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	return mux
}
