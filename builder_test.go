package aastro

import (
	"context"
	"crypto/tls"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/starwalkn/aastro/internal/tlsutil"
)

func TestBuilder_NewRouter(t *testing.T) {
	t.Run("builds a complete bundle from a minimal config", func(t *testing.T) {
		cfg := RoutingConfigSet{
			Service: ServiceConfig{Name: "aastro-test"},
			Routing: RoutingConfig{
				Flows: []FlowConfig{{
					Path:      "/test",
					Method:    http.MethodGet,
					Streaming: true,
					Upstreams: []UpstreamConfig{newTestUpstreamConfig("7001")},
				}},
			},
		}

		bundle, err := NewRouter(context.Background(), cfg, zap.NewNop())
		require.NoError(t, err)
		assert.NotNil(t, bundle.Router)
		assert.NotNil(t, bundle.MeterProvider)  // noop, но не nil
		assert.NotNil(t, bundle.TracerProvider) // noop
		assert.Nil(t, bundle.PromRegistry)      // metrics не enabled
	})

	t.Run("returns an error if a flow fails to compile", func(t *testing.T) {
		cfg := RoutingConfigSet{
			Service: ServiceConfig{Name: "aastro-test"},
			Routing: RoutingConfig{
				Flows: []FlowConfig{{
					Path:      "/bad",
					Streaming: true,
					Upstreams: []UpstreamConfig{newTestUpstreamConfig("7001"), newTestUpstreamConfig("7002")},
				}},
			},
		}

		_, err := NewRouter(context.Background(), cfg, zap.NewNop())
		assert.ErrorContains(t, err, "compile flow")
	})
}

func TestBuilder_InitRateLimiter(t *testing.T) {
	t.Run("returns nil if disabled", func(t *testing.T) {
		cfg := RateLimiterConfig{
			Enabled: false,
			Config:  nil,
		}

		rl, err := initRateLimiter(cfg)
		require.NoError(t, err)
		assert.Nil(t, rl)
	})

	t.Run("returns a configured limiter when enabled", func(t *testing.T) {
		cfg := RateLimiterConfig{
			Enabled: true,
			Config: map[string]interface{}{
				"window": "5s",
				"limit":  10,
			},
		}

		rl, err := initRateLimiter(cfg)
		require.NoError(t, err)
		assert.NotNil(t, rl)
	})
}

func TestBuilder_ParseTrustedProxies(t *testing.T) {
	t.Run("when input is not in CIDR format returns an error", func(t *testing.T) {
		proxies := []string{"127.0.0.1"}

		tp, err := parseTrustedProxies(proxies)
		require.Error(t, err)
		assert.Nil(t, tp)
	})

	t.Run("when input contains valid CIDRs parses all valid CIDRs", func(t *testing.T) {
		proxies := []string{"127.0.0.1/8", "192.168.0.1/32"}

		tp, err := parseTrustedProxies(proxies)
		require.NoError(t, err)
		require.NotNil(t, tp)
		assert.Len(t, tp, 2)
	})
}

func TestBuilder_CompileFlow(t *testing.T) {
	t.Run("rejects streaming flows with multiple upstreams", func(t *testing.T) {
		cfg := FlowConfig{
			Path:      "/builder/test",
			Method:    http.MethodGet,
			Streaming: true,
			Upstreams: []UpstreamConfig{
				newTestUpstreamConfig("7001"),
				newTestUpstreamConfig("7002"),
			},
		}

		f, err := compileFlow(cfg, nil, nil, tlsutil.NewRegistry(), zap.NewNop())
		require.Error(t, err)
		assert.Zero(t, f)
		assert.ErrorContains(t, err, "must have exactly one upstream")
	})

	t.Run("propagates aggregation initialization errors", func(t *testing.T) {
		cfg := FlowConfig{
			Path:      "/builder/test",
			Method:    http.MethodGet,
			Streaming: false,
			Upstreams: []UpstreamConfig{
				newTestUpstreamConfig("7001"),
				newTestUpstreamConfig("7002"),
			},
			Aggregation: &AggregationConfig{
				BestEffort: false,
				Strategy:   "unknown",
			},
		}

		f, err := compileFlow(cfg, nil, nil, tlsutil.NewRegistry(), zap.NewNop())
		require.Error(t, err)
		assert.Zero(t, f)
		assert.ErrorContains(t, err, "init aggregation")
	})

	t.Run("compiles a streaming flow", func(t *testing.T) {
		cfg := FlowConfig{
			Path:      "/builder/test",
			Method:    http.MethodGet,
			Streaming: true,
			Upstreams: []UpstreamConfig{
				newTestUpstreamConfig("7001"),
			},
			Aggregation: &AggregationConfig{
				BestEffort: true,
				Strategy:   strategyArray.String(),
			},
		}

		f, err := compileFlow(cfg, nil, nil, tlsutil.NewRegistry(), zap.NewNop())
		require.NoError(t, err)
		assert.NotZero(t, f)
		assert.Len(t, f.upstreams, 1)
		assert.True(t, f.streaming)
	})

	t.Run("compiles a fan-out flow with aggregation", func(t *testing.T) {
		cfg := FlowConfig{
			Path:      "/builder/test",
			Method:    http.MethodGet,
			Streaming: false,
			Upstreams: []UpstreamConfig{
				newTestUpstreamConfig("7001"),
				newTestUpstreamConfig("7002"),
			},
			Aggregation: &AggregationConfig{
				BestEffort: false,
				Strategy:   strategyNamespace.String(),
			},
		}

		f, err := compileFlow(cfg, nil, nil, tlsutil.NewRegistry(), zap.NewNop())
		require.NoError(t, err)
		assert.NotZero(t, f)
		assert.Len(t, f.upstreams, 2)
		assert.False(t, f.streaming)
	})
}

func TestBuilder_InitAggregation(t *testing.T) {
	t.Run("fails on unknown strategy", func(t *testing.T) {
		cfg := AggregationConfig{
			BestEffort: false,
			Strategy:   "unknown",
			OnConflict: nil,
		}

		agg, err := initAggregation(cfg, nil)
		require.Error(t, err)
		assert.Zero(t, agg)
		assert.ErrorContains(t, err, "unknown aggregation strategy")
	})

	t.Run("with merge strategy fails on unknown conflict policy", func(t *testing.T) {
		cfg := AggregationConfig{
			BestEffort: true,
			Strategy:   strategyMerge.String(),
			OnConflict: &OnConflictConfig{
				Policy: "unknown",
			},
		}

		agg, err := initAggregation(cfg, nil)
		require.Error(t, err)
		assert.Zero(t, agg)
		assert.ErrorContains(t, err, "unknown aggregation conflict policy")
	})

	t.Run("applies default conflict policy for non-merge strategies", func(t *testing.T) {
		cfg := AggregationConfig{
			BestEffort: false,
			Strategy:   strategyArray.String(),
			OnConflict: nil,
		}

		agg, err := initAggregation(cfg, nil)
		require.NoError(t, err)
		assert.Equal(t, cfg.Strategy, agg.strategy.String())
		assert.Equal(t, conflictPolicyOverwrite, agg.conflictPolicy)
	})

	t.Run("with merge strategy and prefer conflict policy fails on non-existent upstream", func(t *testing.T) {
		up := newTestUpstream("test-service:7001")

		cfg := AggregationConfig{
			BestEffort: false,
			Strategy:   strategyMerge.String(),
			OnConflict: &OnConflictConfig{
				Policy:   conflictPolicyPrefer.String(),
				Upstream: "non-existent",
			},
		}

		agg, err := initAggregation(cfg, []upstream{up})
		require.Error(t, err)
		assert.Zero(t, agg)
		assert.ErrorContains(t, err, "preferred upstream for on_conflict policy does not exist")
	})

	t.Run("succeeds with valid config", func(t *testing.T) {
		up := newTestUpstream("test-service:7001")

		cfg := AggregationConfig{
			BestEffort: true,
			Strategy:   strategyMerge.String(),
			OnConflict: &OnConflictConfig{
				Policy: conflictPolicyFirst.String(),
			},
		}

		agg, err := initAggregation(cfg, []upstream{up})
		require.NoError(t, err)
		assert.Equal(t, cfg.Strategy, agg.strategy.String())
		assert.Equal(t, conflictPolicyFirst, agg.conflictPolicy)
		assert.True(t, agg.bestEffort)
	})

	t.Run("buildUpstream successfully builds upstream from valid config", func(t *testing.T) {
		cfg := UpstreamConfig{
			Name:    "",
			Hosts:   AddrList{"test-service:7001", "test-service:7002"},
			Path:    "/builder/test",
			Method:  http.MethodGet,
			Timeout: 5 * time.Second,
		}

		u, err := buildUpstream(cfg, nil, nil, tlsutil.NewRegistry(), zap.NewNop())
		require.NoError(t, err)
		require.NotNil(t, u)
		assert.Equal(t, "get-test-service:7001-test-service:7002", u.name())
	})
}

func TestBuildUpstreamTransport(t *testing.T) {
	t.Run("defaults to HTTP/1.1 only without TLS", func(t *testing.T) {
		tr := buildUpstreamTransport(UpstreamConfig{}, nil)
		assert.Nil(t, tr.Protocols)
	})

	t.Run("negotiates HTTP/2 over TLS by default (auto)", func(t *testing.T) {
		tr := buildUpstreamTransport(UpstreamConfig{}, &tls.Config{})
		require.NotNil(t, tr.Protocols)
		assert.True(t, tr.Protocols.HTTP1())
		assert.True(t, tr.Protocols.HTTP2())
	})

	t.Run("negotiates HTTP/2 over TLS when explicitly on", func(t *testing.T) {
		cfg := UpstreamConfig{Transport: TransportConfig{HTTP2: "on"}}

		tr := buildUpstreamTransport(cfg, &tls.Config{})
		require.NotNil(t, tr.Protocols)
		assert.True(t, tr.Protocols.HTTP1())
		assert.True(t, tr.Protocols.HTTP2())
	})

	t.Run("pins the transport to HTTP/1.1 when off, even over TLS", func(t *testing.T) {
		cfg := UpstreamConfig{Transport: TransportConfig{HTTP2: "off"}}

		tr := buildUpstreamTransport(cfg, &tls.Config{})
		require.NotNil(t, tr.Protocols)
		assert.True(t, tr.Protocols.HTTP1())
		assert.False(t, tr.Protocols.HTTP2())
	})
}

func TestBuildUpstreamPolicy(t *testing.T) {
	t.Run("matches a blacklist entry regardless of its case", func(t *testing.T) {
		// Regression test: entries were used as map keys verbatim, but
		// net/http always stores response headers in net/textproto
		// canonical form - a config value like "x-secret" silently never
		// matched the "X-Secret" key filterHeaders actually sees.
		policy := buildUpstreamPolicy(PolicyConfig{
			HeaderBlacklist: []string{"x-secret", "X-ALREADY-CANONICAL-ISH"},
		})

		_, blocksLowercase := policy.headerBlacklist["X-Secret"]
		assert.True(t, blocksLowercase)

		_, blocksOther := policy.headerBlacklist["X-Already-Canonical-Ish"]
		assert.True(t, blocksOther)
	})
}
