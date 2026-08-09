package openapi

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/starwalkn/aastro"
)

func importDoc(doc *Document, opts ImportOptions) (aastro.Config, []Warning) {
	GinkgoHelper()

	cfg, warnings, err := ToConfig(doc, opts)
	Expect(err).NotTo(HaveOccurred())

	return cfg, warnings
}

var _ = Describe("ToConfig", func() {
	Describe("round-trip with x-aastro extensions", func() {
		It("satisfies export ∘ import ∘ export fixpoint for middleware-free configs", func() {
			original := configWith(true,
				mergeFlow("/api/v1/user/{id}", true, "prefer", func() aastro.UpstreamConfig {
					u := minimalUpstream("billing")
					u.Path = "/v1/users/{id}"
					u.ForwardQueries = []string{"expand"}
					u.ForwardHeaders = []string{"X-Tenant-Id"}
					u.ForwardParams = []string{"id"}
					return u
				}(), minimalUpstream("profile")),
				passthroughFlow("/api/v1/events"),
			)
			original.Gateway.Routing.Flows[0].Aggregation.OnConflict.Upstream = "billing"

			queryFlow := mergeFlow("/api/v1/search", false, "", minimalUpstream("search"))
			queryFlow.Method = "QUERY"
			original.Gateway.Routing.Flows = append(original.Gateway.Routing.Flows, queryFlow)

			firstDoc, _ := generate(original, Options{Extensions: true})

			imported, warnings := importDoc(firstDoc, ImportOptions{})
			Expect(warnings).To(HaveLen(1))
			Expect(warnings[0].Message).To(ContainSubstring("rate limiter enabled"))

			secondDoc, _ := generate(imported, Options{Extensions: true})

			Expect(secondDoc).To(Equal(firstDoc))
		})

		It("reconstructs aggregation, upstreams, and timeouts losslessly", func() {
			u := minimalUpstream("billing")
			u.Timeout = 7 * time.Second

			original := configWith(false, mergeFlow("/a/{id}", true, "prefer", u, minimalUpstream("profile")))
			original.Gateway.Routing.Flows[0].Aggregation.OnConflict.Upstream = "billing"

			doc, _ := generate(original, Options{Extensions: true})
			cfg, _ := importDoc(doc, ImportOptions{})

			Expect(cfg.Gateway.Routing.Flows).To(HaveLen(1))

			flow := cfg.Gateway.Routing.Flows[0]
			Expect(flow.Path).To(Equal("/a/{id}"))
			Expect(flow.Aggregation.Strategy).To(Equal("merge"))
			Expect(flow.Aggregation.BestEffort).To(BeTrue())
			Expect(flow.Aggregation.OnConflict.Policy).To(Equal("prefer"))
			Expect(flow.Aggregation.OnConflict.Upstream).To(Equal("billing"))
			Expect(flow.Upstreams[0].Timeout).To(Equal(7 * time.Second))
			Expect(flow.Upstreams[1].Timeout).To(Equal(3 * time.Second))
		})

		It("restores passthrough flows without aggregation", func() {
			doc, _ := generate(configWith(false, passthroughFlow("/s")), Options{Extensions: true})
			cfg, _ := importDoc(doc, ImportOptions{})

			flow := cfg.Gateway.Routing.Flows[0]
			Expect(flow.Passthrough).To(BeTrue())
			Expect(flow.Aggregation).To(BeNil())
		})

		It("restores QUERY flows from x-aastro-query", func() {
			f := mergeFlow("/search", false, "", minimalUpstream("q"))
			f.Method = "QUERY"

			doc, _ := generate(configWith(false, f), Options{Extensions: true})
			cfg, _ := importDoc(doc, ImportOptions{})

			Expect(cfg.Gateway.Routing.Flows[0].Method).To(Equal("QUERY"))
		})

		It("drops middlewares with a warning instead of emitting broken configs", func() {
			f := mergeFlow("/secured", false, "", minimalUpstream("u"))
			f.Middlewares = []aastro.MiddlewareConfig{
				{Name: "recoverer", Source: "builtin"},
				authMiddlewareConfig(map[string]interface{}{"issuer": "https://idp"}),
			}

			doc, _ := generate(configWith(false, f), Options{Extensions: true})
			cfg, warnings := importDoc(doc, ImportOptions{})

			Expect(cfg.Gateway.Routing.Flows[0].Middlewares).To(BeEmpty())
			Expect(warnings).To(ContainElement(SatisfyAll(
				HaveField("Flow", "GET /secured"),
				HaveField("Message", ContainSubstring("recoverer, auth")),
			)))
		})

		It("infers the rate limiter from 429 responses", func() {
			doc, _ := generate(configWith(true, mergeFlow("/a", false, "", minimalUpstream("u"))), Options{Extensions: true})
			cfg, warnings := importDoc(doc, ImportOptions{})

			Expect(cfg.Gateway.Routing.RateLimiter.Enabled).To(BeTrue())
			Expect(cfg.Gateway.Routing.RateLimiter.Config).To(HaveKey("limit"))
			Expect(warnings).To(ContainElement(HaveField("Message", ContainSubstring("rate limiter enabled"))))

			unlimitedDoc, _ := generate(configWith(false, mergeFlow("/a", false, "", minimalUpstream("u"))), Options{Extensions: true})
			unlimitedCfg, _ := importDoc(unlimitedDoc, ImportOptions{})

			Expect(unlimitedCfg.Gateway.Routing.RateLimiter.Enabled).To(BeFalse())
		})
	})

	Describe("scaffolding foreign documents", func() {
		foreignDoc := func() *Document {
			return &Document{
				OpenAPI: "3.1.0",
				Info:    Info{Title: "petstore", Version: "1.0.0"},
				Servers: []Server{{URL: "https://petstore.example.com"}},
				Paths: map[string]*PathItem{
					"/pets/{petId}": {
						Get: &Operation{
							OperationID: "getPetById",
							Parameters: []Parameter{
								{Name: "petId", In: "path", Required: true},
								{Name: "verbose", In: "query"},
								{Name: "X-Store-Id", In: "header"},
							},
							Security:  []map[string][]string{{"api_key": {}}},
							Responses: map[string]*Response{"200": {Description: "ok"}},
						},
					},
				},
			}
		}

		It("builds a single-upstream envelope flow from an operation", func() {
			cfg, warnings := importDoc(foreignDoc(), ImportOptions{})

			Expect(cfg.Schema).To(Equal("v1"))
			Expect(cfg.Gateway.Service.Name).To(Equal("petstore"))
			Expect(cfg.Gateway.Server.Port).To(Equal(defaultServerPort))

			flow := cfg.Gateway.Routing.Flows[0]
			Expect(flow.Path).To(Equal("/pets/{petId}"))
			Expect(flow.Method).To(Equal("GET"))
			Expect(flow.Passthrough).To(BeFalse())
			Expect(flow.Aggregation.Strategy).To(Equal("array"))

			up := flow.Upstreams[0]
			Expect(up.Name).To(Equal("getpetbyid"))
			Expect(up.Hosts).To(HaveExactElements("https://petstore.example.com"))
			Expect(up.Path).To(Equal("/pets/{petId}"))
			Expect(up.ForwardQueries).To(HaveExactElements("verbose"))
			Expect(up.ForwardHeaders).To(HaveExactElements("X-Store-Id"))

			Expect(warnings).To(ContainElement(HaveField("Message", ContainSubstring("security requirement"))))
		})

		It("prefers --default-host over servers[] and warns when neither exists", func() {
			cfg, _ := importDoc(foreignDoc(), ImportOptions{DefaultHost: "https://internal:8080"})
			Expect(cfg.Gateway.Routing.Flows[0].Upstreams[0].Hosts).To(HaveExactElements("https://internal:8080"))

			bare := foreignDoc()
			bare.Servers = nil

			cfg, warnings := importDoc(bare, ImportOptions{})
			Expect(cfg.Gateway.Routing.Flows[0].Upstreams[0].Hosts).To(HaveExactElements(placeholderHost))
			Expect(warnings).To(ContainElement(HaveField("Message", ContainSubstring("placeholder host"))))
		})

		It("scaffolds passthrough flows when requested", func() {
			cfg, _ := importDoc(foreignDoc(), ImportOptions{Mode: "passthrough"})

			flow := cfg.Gateway.Routing.Flows[0]
			Expect(flow.Passthrough).To(BeTrue())
			Expect(flow.Aggregation).To(BeNil())
		})

		It("orders flows deterministically by path and method", func() {
			doc := foreignDoc()
			doc.Paths["/a"] = &PathItem{
				Post: &Operation{Responses: map[string]*Response{"200": {Description: "ok"}}},
				Get:  &Operation{Responses: map[string]*Response{"200": {Description: "ok"}}},
			}

			first, _ := importDoc(doc, ImportOptions{})
			second, _ := importDoc(doc, ImportOptions{})

			Expect(second).To(Equal(first))
			Expect(first.Gateway.Routing.Flows[0].Path).To(Equal("/a"))
			Expect(first.Gateway.Routing.Flows[0].Method).To(Equal("GET"))
			Expect(first.Gateway.Routing.Flows[1].Method).To(Equal("POST"))
		})
	})

	Describe("policy, transport, and TLS round-trip", func() {
		richUpstream := func() aastro.UpstreamConfig {
			u := minimalUpstream("billing")
			u.TLS = aastro.TLSConfig{Enabled: true}
			u.Transport = aastro.TransportConfig{MaxIdleConns: 100, MaxIdleConnsPerHost: 50, IdleConnTimeout: 90 * time.Second}
			u.Policy = aastro.PolicyConfig{
				HeaderBlacklist:     []string{"X-Internal-Token"},
				AllowedStatuses:     []int{200, 204},
				RequireBody:         true,
				MaxResponseBodySize: 1 << 20,
				FollowRedirects:     true,
				RetryConfig: aastro.RetryConfig{
					MaxRetries:      3,
					RetryOnStatuses: []int{500, 502, 503},
					BackoffDelay:    200 * time.Millisecond,
				},
				CircuitBreakerConfig: aastro.CircuitBreakerConfig{Enabled: true, MaxFailures: 5, ResetTimeout: 10 * time.Second},
				LoadBalancingConfig:  aastro.LoadBalancingConfig{Mode: "least_conns"},
			}
			return u
		}

		It("restores policy and transport losslessly and keeps the fixpoint", func() {
			original := configWith(false, mergeFlow("/a", false, "", richUpstream()))

			firstDoc, _ := generate(original, Options{Extensions: true})
			imported, warnings := importDoc(firstDoc, ImportOptions{})

			up := imported.Gateway.Routing.Flows[0].Upstreams[0]
			Expect(up.Policy.RetryConfig.MaxRetries).To(Equal(3))
			Expect(up.Policy.RetryConfig.RetryOnStatuses).To(HaveExactElements(500, 502, 503))
			Expect(up.Policy.RetryConfig.BackoffDelay).To(Equal(200 * time.Millisecond))
			Expect(up.Policy.CircuitBreakerConfig.Enabled).To(BeTrue())
			Expect(up.Policy.CircuitBreakerConfig.ResetTimeout).To(Equal(10 * time.Second))
			Expect(up.Policy.LoadBalancingConfig.Mode).To(Equal("least_conns"))
			Expect(up.Policy.HeaderBlacklist).To(HaveExactElements("X-Internal-Token"))
			Expect(up.Policy.MaxResponseBodySize).To(Equal(int64(1 << 20)))
			Expect(up.Policy.FollowRedirects).To(BeTrue())
			Expect(up.Transport.MaxIdleConns).To(Equal(100))
			Expect(up.Transport.IdleConnTimeout).To(Equal(90 * time.Second))
			Expect(up.TLS.Enabled).To(BeTrue())
			Expect(warnings).To(ContainElement(HaveField("Message", ContainSubstring("system roots"))))

			secondDoc, _ := generate(imported, Options{Extensions: true})
			Expect(secondDoc).To(Equal(firstDoc))
		})

		It("warns about plugins by name", func() {
			f := mergeFlow("/a", false, "", minimalUpstream("u"))
			f.Plugins = []aastro.PluginConfig{
				{Name: "snakeify", Source: "builtin"},
				{Name: "tenant_resolver", Source: "file", Path: "/plugins/"},
			}

			doc, _ := generate(configWith(false, f), Options{Extensions: true})
			cfg, warnings := importDoc(doc, ImportOptions{})

			Expect(cfg.Gateway.Routing.Flows[0].Plugins).To(BeEmpty())
			Expect(warnings).To(ContainElement(HaveField("Message", ContainSubstring("snakeify, tenant_resolver"))))
		})
	})

	Describe("scaffold heuristics", func() {
		It("detects streamed operations and scaffolds them as passthrough", func() {
			doc := foreign()
			doc.Paths["/stream"] = &PathItem{
				Get: &Operation{
					Responses: map[string]*Response{
						"200": {Description: "stream", Content: map[string]MediaType{"*/*": {}}},
					},
				},
			}

			cfg, warnings := importDoc(doc, ImportOptions{})

			var streamFlow *aastro.FlowConfig

			for i := range cfg.Gateway.Routing.Flows {
				if cfg.Gateway.Routing.Flows[i].Path == "/stream" {
					streamFlow = &cfg.Gateway.Routing.Flows[i]
				}
			}

			Expect(streamFlow).NotTo(BeNil())
			Expect(streamFlow.Passthrough).To(BeTrue())
			Expect(streamFlow.Aggregation).To(BeNil())
			Expect(warnings).To(ContainElement(SatisfyAll(
				HaveField("Flow", "GET /stream"),
				HaveField("Message", ContainSubstring("passthrough")),
			)))
		})

		It("warns when an aastroctl-generated document lacks per-operation extensions", func() {
			doc := foreign()
			doc.XAastro = &RootExtension{Schema: "v1", Generator: "aastroctl/0.7.0"}

			_, warnings := importDoc(doc, ImportOptions{})

			Expect(warnings).To(ContainElement(HaveField("Message", ContainSubstring("--extensions"))))
		})

		It("does not warn about extensions for foreign or extension-carrying documents", func() {
			_, warnings := importDoc(foreign(), ImportOptions{})
			for _, w := range warnings {
				Expect(w.Message).NotTo(ContainSubstring("--extensions"))
			}

			doc, _ := generate(configWith(false, passthroughFlow("/s")), Options{Extensions: true})
			_, warnings = importDoc(doc, ImportOptions{})
			for _, w := range warnings {
				Expect(w.Message).NotTo(ContainSubstring("--extensions"))
			}
		})
	})

	Describe("input validation", func() {
		It("rejects nil documents", func() {
			_, _, err := ToConfig(nil, ImportOptions{})
			Expect(err).To(MatchError("nil document"))
		})

		It("rejects documents without operations", func() {
			_, _, err := ToConfig(&Document{Paths: map[string]*PathItem{}}, ImportOptions{})
			Expect(err).To(MatchError(ContainSubstring("no operations")))
		})

		It("rejects unknown import modes", func() {
			_, _, err := ToConfig(foreign(), ImportOptions{Mode: "hybrid"})
			Expect(err).To(MatchError(ContainSubstring("unsupported import mode")))
		})
	})
})

func foreign() *Document {
	return &Document{
		Paths: map[string]*PathItem{
			"/x": {Get: &Operation{Responses: map[string]*Response{"200": {Description: "ok"}}}},
		},
	}
}
