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
	// upstream (see Router.dispatch) — a single-upstream flow is proxied
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
})
