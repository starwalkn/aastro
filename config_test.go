package aastro

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("ValidateConfig", func() {
	// Aggregation is only reached by the router when a flow has more than one
	// upstream (see Router.dispatch) - a single-upstream flow is proxied
	// directly, streaming or not, and never aggregates.
	Describe("aggregation requirement", func() {
		It("does not require aggregation for a single-upstream flow", func() {
			cfg := minimalValidConfig(FlowConfig{
				Path:      "/a",
				Method:    http.MethodGet,
				Upstreams: []UpstreamConfig{testUpstreamConfig("7001")},
			})

			Expect(ValidateConfig(&cfg)).To(Succeed())
		})

		It("does not require aggregation for a streaming flow", func() {
			cfg := minimalValidConfig(FlowConfig{
				Path:      "/a",
				Method:    http.MethodGet,
				Streaming: true,
				Upstreams: []UpstreamConfig{testUpstreamConfig("7001")},
			})

			Expect(ValidateConfig(&cfg)).To(Succeed())
		})

		It("requires aggregation for a multi-upstream flow", func() {
			cfg := minimalValidConfig(FlowConfig{
				Path:   "/a",
				Method: http.MethodGet,
				Upstreams: []UpstreamConfig{
					testUpstreamConfig("7001"),
					testUpstreamConfig("7002"),
				},
			})

			Expect(ValidateConfig(&cfg)).To(MatchError(ContainSubstring("aggregation is required")))
		})

		It("accepts a multi-upstream flow that declares aggregation", func() {
			cfg := minimalValidConfig(FlowConfig{
				Path:   "/a",
				Method: http.MethodGet,
				Upstreams: []UpstreamConfig{
					testUpstreamConfig("7001"),
					testUpstreamConfig("7002"),
				},
				Aggregation: &AggregationConfig{Strategy: "array"},
			})

			Expect(ValidateConfig(&cfg)).To(Succeed())
		})
	})

	// The gateway never speaks cleartext HTTP/2 (h2c) - http2: on only makes
	// sense where there's TLS to negotiate it over.
	Describe("http2", func() {
		It("rejects server http2: on without server TLS enabled", func() {
			cfg := minimalValidConfig(FlowConfig{
				Path:      "/a",
				Method:    http.MethodGet,
				Upstreams: []UpstreamConfig{testUpstreamConfig("7001")},
			})
			cfg.Gateway.Server.HTTP2 = "on"

			Expect(ValidateConfig(&cfg)).To(MatchError(ContainSubstring("gateway.server.http2")))
		})

		It("accepts server http2: on with server TLS enabled", func() {
			cfg := minimalValidConfig(FlowConfig{
				Path:      "/a",
				Method:    http.MethodGet,
				Upstreams: []UpstreamConfig{testUpstreamConfig("7001")},
			})
			cfg.Gateway.Server.HTTP2 = "on"
			cfg.Gateway.Server.TLS = ServerTLSConfig{
				Enabled:  true,
				CertFile: "server.crt",
				KeyFile:  "server.key",
			}

			Expect(ValidateConfig(&cfg)).To(Succeed())
		})

		It("accepts server http2: off regardless of TLS", func() {
			cfg := minimalValidConfig(FlowConfig{
				Path:      "/a",
				Method:    http.MethodGet,
				Upstreams: []UpstreamConfig{testUpstreamConfig("7001")},
			})
			cfg.Gateway.Server.HTTP2 = "off"

			Expect(ValidateConfig(&cfg)).To(Succeed())
		})

		It("rejects upstream transport.http2: on without upstream TLS enabled", func() {
			upstream := testUpstreamConfig("7001")
			upstream.Transport.HTTP2 = "on"

			cfg := minimalValidConfig(FlowConfig{
				Path:      "/a",
				Method:    http.MethodGet,
				Upstreams: []UpstreamConfig{upstream},
			})

			Expect(ValidateConfig(&cfg)).To(MatchError(ContainSubstring(`upstream "test_service_7001": transport.http2`)))
		})

		It("accepts upstream transport.http2: on with upstream TLS enabled", func() {
			upstream := testUpstreamConfig("7001")
			upstream.Transport.HTTP2 = "on"
			upstream.TLS = TLSConfig{Enabled: true}

			cfg := minimalValidConfig(FlowConfig{
				Path:      "/a",
				Method:    http.MethodGet,
				Upstreams: []UpstreamConfig{upstream},
			})

			Expect(ValidateConfig(&cfg)).To(Succeed())
		})
	})
})
