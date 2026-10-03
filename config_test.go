package aastro

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func minimalValidConfig(flows ...FlowConfig) Config {
	return Config{
		Schema: "v1",
		Gateway: GatewayConfig{
			Server:  ServerConfig{Port: 7805},
			Admin:   AdminConfig{Port: 9090},
			Routing: RoutingConfig{Flows: flows},
		},
	}
}

// Aggregation is only reached by the router when a flow has more than one
// upstream (see Router.dispatch) - a single-upstream flow is proxied
// directly, streaming or not, and never aggregates.
func TestValidateConfig_AggregationRequirement(t *testing.T) {
	t.Run("does not require aggregation for a single-upstream flow", func(t *testing.T) {
		cfg := minimalValidConfig(FlowConfig{
			Path:      "/a",
			Method:    http.MethodGet,
			Upstreams: []UpstreamConfig{newTestUpstreamConfig("7001")},
		})

		assert.NoError(t, ValidateConfig(&cfg))
	})

	t.Run("does not require aggregation for a streaming flow", func(t *testing.T) {
		cfg := minimalValidConfig(FlowConfig{
			Path:      "/a",
			Method:    http.MethodGet,
			Streaming: true,
			Upstreams: []UpstreamConfig{newTestUpstreamConfig("7001")},
		})

		assert.NoError(t, ValidateConfig(&cfg))
	})

	t.Run("requires aggregation for a multi-upstream flow", func(t *testing.T) {
		cfg := minimalValidConfig(FlowConfig{
			Path:   "/a",
			Method: http.MethodGet,
			Upstreams: []UpstreamConfig{
				newTestUpstreamConfig("7001"),
				newTestUpstreamConfig("7002"),
			},
		})

		assert.ErrorContains(t, ValidateConfig(&cfg), "aggregation is required")
	})

	t.Run("accepts a multi-upstream flow that declares aggregation", func(t *testing.T) {
		cfg := minimalValidConfig(FlowConfig{
			Path:   "/a",
			Method: http.MethodGet,
			Upstreams: []UpstreamConfig{
				newTestUpstreamConfig("7001"),
				newTestUpstreamConfig("7002"),
			},
			Aggregation: &AggregationConfig{Strategy: "array"},
		})

		assert.NoError(t, ValidateConfig(&cfg))
	})
}

// The gateway never speaks cleartext HTTP/2 (h2c) - http2: on only makes
// sense where there's TLS to negotiate it over.
func TestValidateConfig_HTTP2(t *testing.T) {
	t.Run("rejects server http2: on without server TLS enabled", func(t *testing.T) {
		cfg := minimalValidConfig(FlowConfig{
			Path:      "/a",
			Method:    http.MethodGet,
			Upstreams: []UpstreamConfig{newTestUpstreamConfig("7001")},
		})
		cfg.Gateway.Server.HTTP2 = "on"

		assert.ErrorContains(t, ValidateConfig(&cfg), "gateway.server.http2")
	})

	t.Run("accepts server http2: on with server TLS enabled", func(t *testing.T) {
		cfg := minimalValidConfig(FlowConfig{
			Path:      "/a",
			Method:    http.MethodGet,
			Upstreams: []UpstreamConfig{newTestUpstreamConfig("7001")},
		})
		cfg.Gateway.Server.HTTP2 = "on"
		cfg.Gateway.Server.TLS = ServerTLSConfig{
			Enabled:  true,
			CertFile: "server.crt",
			KeyFile:  "server.key",
		}

		assert.NoError(t, ValidateConfig(&cfg))
	})

	t.Run("accepts server http2: off regardless of TLS", func(t *testing.T) {
		cfg := minimalValidConfig(FlowConfig{
			Path:      "/a",
			Method:    http.MethodGet,
			Upstreams: []UpstreamConfig{newTestUpstreamConfig("7001")},
		})
		cfg.Gateway.Server.HTTP2 = "off"

		assert.NoError(t, ValidateConfig(&cfg))
	})

	t.Run("rejects upstream transport.http2: on without upstream TLS enabled", func(t *testing.T) {
		upstream := newTestUpstreamConfig("7001")
		upstream.Transport.HTTP2 = "on"

		cfg := minimalValidConfig(FlowConfig{
			Path:      "/a",
			Method:    http.MethodGet,
			Upstreams: []UpstreamConfig{upstream},
		})

		assert.ErrorContains(t, ValidateConfig(&cfg), `upstream "test_service_7001": transport.http2`)
	})

	t.Run("accepts upstream transport.http2: on with upstream TLS enabled", func(t *testing.T) {
		upstream := newTestUpstreamConfig("7001")
		upstream.Transport.HTTP2 = "on"
		upstream.TLS = TLSConfig{Enabled: true}

		cfg := minimalValidConfig(FlowConfig{
			Path:      "/a",
			Method:    http.MethodGet,
			Upstreams: []UpstreamConfig{upstream},
		})

		assert.NoError(t, ValidateConfig(&cfg))
	})
}
