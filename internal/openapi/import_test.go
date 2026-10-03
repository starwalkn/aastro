package openapi

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/voidrunner3074/aastro"
)

func importDoc(t *testing.T, doc *Document, opts ImportOptions) (aastro.Config, []Warning) {
	t.Helper()

	cfg, warnings, err := ToConfig(doc, opts)
	require.NoError(t, err)

	return cfg, warnings
}

// assertWarningMatches fails with the full warning list if none of them
// satisfy match - more useful on failure than a bare assert.True would be.
func assertWarningMatches(t *testing.T, warnings []Warning, match func(Warning) bool) {
	t.Helper()

	assert.True(t, slices.ContainsFunc(warnings, match), "no warning matched, got: %+v", warnings)
}

func TestToConfig(t *testing.T) {
	t.Run("round-trip with x-aastro extensions", func(t *testing.T) {
		t.Run("satisfies export compose import compose export fixpoint for middleware-free configs", func(t *testing.T) {
			original := configWith(true,
				mergeFlow("/api/v1/user/{id}", true, "prefer", func() aastro.UpstreamConfig {
					u := minimalUpstream("billing")
					u.Path = "/v1/users/{id}"
					u.ForwardQueries = []string{"expand"}
					u.ForwardHeaders = []string{"X-Tenant-Id"}
					u.ForwardParams = []string{"id"}
					return u
				}(), minimalUpstream("profile")),
				streamingFlow("/api/v1/events"),
			)
			original.Gateway.Routing.Flows[0].Aggregation.OnConflict.Upstream = "billing"

			queryFlow := mergeFlow("/api/v1/search", false, "", minimalUpstream("search"))
			queryFlow.Method = "QUERY"
			original.Gateway.Routing.Flows = append(original.Gateway.Routing.Flows, queryFlow)

			firstDoc, _ := generate(t, original, Options{Extensions: true})

			imported, warnings := importDoc(t, firstDoc, ImportOptions{})
			require.Len(t, warnings, 1)
			assert.Contains(t, warnings[0].Message, "rate limiter enabled")

			secondDoc, _ := generate(t, imported, Options{Extensions: true})

			assert.Equal(t, firstDoc, secondDoc)
		})

		t.Run("reconstructs aggregation, upstreams, and timeouts losslessly", func(t *testing.T) {
			u := minimalUpstream("billing")
			u.Timeout = 7 * time.Second

			original := configWith(false, mergeFlow("/a/{id}", true, "prefer", u, minimalUpstream("profile")))
			original.Gateway.Routing.Flows[0].Aggregation.OnConflict.Upstream = "billing"

			doc, _ := generate(t, original, Options{Extensions: true})
			cfg, _ := importDoc(t, doc, ImportOptions{})

			require.Len(t, cfg.Gateway.Routing.Flows, 1)

			flow := cfg.Gateway.Routing.Flows[0]
			assert.Equal(t, "/a/{id}", flow.Path)
			assert.Equal(t, "merge", flow.Aggregation.Strategy)
			assert.True(t, flow.Aggregation.BestEffort)
			assert.Equal(t, "prefer", flow.Aggregation.OnConflict.Policy)
			assert.Equal(t, "billing", flow.Aggregation.OnConflict.Upstream)
			assert.Equal(t, 7*time.Second, flow.Upstreams[0].Timeout)
			assert.Equal(t, 3*time.Second, flow.Upstreams[1].Timeout)
		})

		t.Run("restores streaming flows without aggregation", func(t *testing.T) {
			doc, _ := generate(t, configWith(false, streamingFlow("/s")), Options{Extensions: true})
			cfg, _ := importDoc(t, doc, ImportOptions{})

			flow := cfg.Gateway.Routing.Flows[0]
			assert.True(t, flow.Streaming)
			assert.Nil(t, flow.Aggregation)
		})

		t.Run("restores QUERY flows from x-aastro-query", func(t *testing.T) {
			f := mergeFlow("/search", false, "", minimalUpstream("q"))
			f.Method = "QUERY"

			doc, _ := generate(t, configWith(false, f), Options{Extensions: true})
			cfg, _ := importDoc(t, doc, ImportOptions{})

			assert.Equal(t, "QUERY", cfg.Gateway.Routing.Flows[0].Method)
		})

		t.Run("drops middlewares with a warning instead of emitting broken configs", func(t *testing.T) {
			f := mergeFlow("/secured", false, "", minimalUpstream("u"))
			f.Middlewares = []aastro.MiddlewareConfig{
				{Name: "recoverer", Source: "builtin"},
				authMiddlewareConfig(map[string]interface{}{"issuer": "https://idp"}),
			}

			doc, _ := generate(t, configWith(false, f), Options{Extensions: true})
			cfg, warnings := importDoc(t, doc, ImportOptions{})

			assert.Empty(t, cfg.Gateway.Routing.Flows[0].Middlewares)
			assertWarningMatches(t, warnings, func(w Warning) bool {
				return w.Flow == "GET /secured" && strings.Contains(w.Message, "recoverer, auth")
			})
		})

		t.Run("infers the rate limiter from 429 responses", func(t *testing.T) {
			doc, _ := generate(t, configWith(true, mergeFlow("/a", false, "", minimalUpstream("u"))), Options{Extensions: true})
			cfg, warnings := importDoc(t, doc, ImportOptions{})

			assert.True(t, cfg.Gateway.Routing.RateLimiter.Enabled)
			assert.Contains(t, cfg.Gateway.Routing.RateLimiter.Config, "limit")
			assertWarningMatches(t, warnings, func(w Warning) bool {
				return strings.Contains(w.Message, "rate limiter enabled")
			})

			unlimitedDoc, _ := generate(t, configWith(false, mergeFlow("/a", false, "", minimalUpstream("u"))), Options{Extensions: true})
			unlimitedCfg, _ := importDoc(t, unlimitedDoc, ImportOptions{})

			assert.False(t, unlimitedCfg.Gateway.Routing.RateLimiter.Enabled)
		})
	})

	t.Run("scaffolding foreign documents", func(t *testing.T) {
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

		t.Run("builds a single-upstream proxy flow from an operation", func(t *testing.T) {
			cfg, warnings := importDoc(t, foreignDoc(), ImportOptions{})

			assert.Equal(t, "v1", cfg.Schema)
			assert.Equal(t, "petstore", cfg.Gateway.Service.Name)
			assert.Equal(t, defaultServerPort, cfg.Gateway.Server.Port)

			flow := cfg.Gateway.Routing.Flows[0]
			assert.Equal(t, "/pets/{petId}", flow.Path)
			assert.Equal(t, "GET", flow.Method)
			assert.False(t, flow.Streaming)
			// A scaffolded flow always has exactly one upstream, so it is
			// proxied directly and never aggregates (see Router.dispatch).
			assert.Nil(t, flow.Aggregation)

			up := flow.Upstreams[0]
			assert.Equal(t, "getpetbyid", up.Name)
			assert.Equal(t, aastro.AddrList{"https://petstore.example.com"}, up.Hosts)
			assert.Equal(t, "/pets/{petId}", up.Path)
			assert.Equal(t, []string{"verbose"}, up.ForwardQueries)
			assert.Equal(t, []string{"X-Store-Id"}, up.ForwardHeaders)

			assertWarningMatches(t, warnings, func(w Warning) bool {
				return strings.Contains(w.Message, "security requirement")
			})
		})

		t.Run("prefers --default-host over servers[] and warns when neither exists", func(t *testing.T) {
			cfg, _ := importDoc(t, foreignDoc(), ImportOptions{DefaultHost: "https://internal:8080"})
			assert.Equal(t, aastro.AddrList{"https://internal:8080"}, cfg.Gateway.Routing.Flows[0].Upstreams[0].Hosts)

			bare := foreignDoc()
			bare.Servers = nil

			cfg, warnings := importDoc(t, bare, ImportOptions{})
			assert.Equal(t, aastro.AddrList{placeholderHost}, cfg.Gateway.Routing.Flows[0].Upstreams[0].Hosts)
			assertWarningMatches(t, warnings, func(w Warning) bool {
				return strings.Contains(w.Message, "placeholder host")
			})
		})

		t.Run("scaffolds streaming flows when requested", func(t *testing.T) {
			cfg, _ := importDoc(t, foreignDoc(), ImportOptions{Mode: "streaming"})

			flow := cfg.Gateway.Routing.Flows[0]
			assert.True(t, flow.Streaming)
			assert.Nil(t, flow.Aggregation)
		})

		t.Run("orders flows deterministically by path and method", func(t *testing.T) {
			doc := foreignDoc()
			doc.Paths["/a"] = &PathItem{
				Post: &Operation{Responses: map[string]*Response{"200": {Description: "ok"}}},
				Get:  &Operation{Responses: map[string]*Response{"200": {Description: "ok"}}},
			}

			first, _ := importDoc(t, doc, ImportOptions{})
			second, _ := importDoc(t, doc, ImportOptions{})

			assert.Equal(t, first, second)
			assert.Equal(t, "/a", first.Gateway.Routing.Flows[0].Path)
			assert.Equal(t, "GET", first.Gateway.Routing.Flows[0].Method)
			assert.Equal(t, "POST", first.Gateway.Routing.Flows[1].Method)
		})
	})

	t.Run("policy, transport, and TLS round-trip", func(t *testing.T) {
		richUpstream := func() aastro.UpstreamConfig {
			u := minimalUpstream("billing")
			u.TLS = aastro.TLSConfig{Enabled: true}
			u.Transport = aastro.TransportConfig{MaxIdleConns: 100, MaxIdleConnsPerHost: 50, IdleConnTimeout: 90 * time.Second}
			u.Policy = aastro.PolicyConfig{
				HeaderBlacklist:     []string{"X-Internal-Token"},
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

		t.Run("restores policy and transport losslessly and keeps the fixpoint", func(t *testing.T) {
			original := configWith(false, mergeFlow("/a", false, "", richUpstream()))

			firstDoc, _ := generate(t, original, Options{Extensions: true})
			imported, warnings := importDoc(t, firstDoc, ImportOptions{})

			up := imported.Gateway.Routing.Flows[0].Upstreams[0]
			assert.Equal(t, 3, up.Policy.RetryConfig.MaxRetries)
			assert.Equal(t, []int{500, 502, 503}, up.Policy.RetryConfig.RetryOnStatuses)
			assert.Equal(t, 200*time.Millisecond, up.Policy.RetryConfig.BackoffDelay)
			assert.True(t, up.Policy.CircuitBreakerConfig.Enabled)
			assert.Equal(t, 10*time.Second, up.Policy.CircuitBreakerConfig.ResetTimeout)
			assert.Equal(t, "least_conns", up.Policy.LoadBalancingConfig.Mode)
			assert.Equal(t, []string{"X-Internal-Token"}, up.Policy.HeaderBlacklist)
			assert.Equal(t, int64(1<<20), up.Policy.MaxResponseBodySize)
			assert.True(t, up.Policy.FollowRedirects)
			assert.Equal(t, 100, up.Transport.MaxIdleConns)
			assert.Equal(t, 90*time.Second, up.Transport.IdleConnTimeout)
			assert.True(t, up.TLS.Enabled)
			assertWarningMatches(t, warnings, func(w Warning) bool {
				return strings.Contains(w.Message, "system roots")
			})

			secondDoc, _ := generate(t, imported, Options{Extensions: true})
			assert.Equal(t, firstDoc, secondDoc)
		})

		t.Run("warns about plugins by name", func(t *testing.T) {
			f := mergeFlow("/a", false, "", minimalUpstream("u"))
			f.Plugins = []aastro.PluginConfig{
				{Name: "snakeify", Source: "builtin"},
				{Name: "tenant_resolver", Source: "file", Path: "/plugins/"},
			}

			doc, _ := generate(t, configWith(false, f), Options{Extensions: true})
			cfg, warnings := importDoc(t, doc, ImportOptions{})

			assert.Empty(t, cfg.Gateway.Routing.Flows[0].Plugins)
			assertWarningMatches(t, warnings, func(w Warning) bool {
				return strings.Contains(w.Message, "snakeify, tenant_resolver")
			})
		})
	})

	t.Run("scaffold heuristics", func(t *testing.T) {
		t.Run("detects streamed operations and scaffolds them as streaming", func(t *testing.T) {
			doc := foreign()
			doc.Paths["/stream"] = &PathItem{
				Get: &Operation{
					Responses: map[string]*Response{
						"200": {Description: "stream", Content: map[string]MediaType{"*/*": {}}},
					},
				},
			}

			cfg, warnings := importDoc(t, doc, ImportOptions{})

			var streamFlow *aastro.FlowConfig

			for i := range cfg.Gateway.Routing.Flows {
				if cfg.Gateway.Routing.Flows[i].Path == "/stream" {
					streamFlow = &cfg.Gateway.Routing.Flows[i]
				}
			}

			require.NotNil(t, streamFlow)
			assert.True(t, streamFlow.Streaming)
			assert.Nil(t, streamFlow.Aggregation)
			assertWarningMatches(t, warnings, func(w Warning) bool {
				return w.Flow == "GET /stream" && strings.Contains(w.Message, "streaming")
			})
		})

		t.Run("warns when an aastroctl-generated document lacks per-operation extensions", func(t *testing.T) {
			doc := foreign()
			doc.XAastro = &RootExtension{Schema: "v1", Generator: "aastroctl/0.7.0"}

			_, warnings := importDoc(t, doc, ImportOptions{})

			assertWarningMatches(t, warnings, func(w Warning) bool {
				return strings.Contains(w.Message, "--extensions")
			})
		})

		t.Run("does not warn about extensions for foreign or extension-carrying documents", func(t *testing.T) {
			_, warnings := importDoc(t, foreign(), ImportOptions{})
			for _, w := range warnings {
				assert.NotContains(t, w.Message, "--extensions")
			}

			doc, _ := generate(t, configWith(false, streamingFlow("/s")), Options{Extensions: true})
			_, warnings = importDoc(t, doc, ImportOptions{})
			for _, w := range warnings {
				assert.NotContains(t, w.Message, "--extensions")
			}
		})
	})

	t.Run("input validation", func(t *testing.T) {
		t.Run("rejects nil documents", func(t *testing.T) {
			_, _, err := ToConfig(nil, ImportOptions{})
			assert.EqualError(t, err, "nil document")
		})

		t.Run("rejects documents without operations", func(t *testing.T) {
			_, _, err := ToConfig(&Document{Paths: map[string]*PathItem{}}, ImportOptions{})
			assert.ErrorContains(t, err, "no operations")
		})

		t.Run("rejects unknown import modes", func(t *testing.T) {
			_, _, err := ToConfig(foreign(), ImportOptions{Mode: "hybrid"})
			assert.ErrorContains(t, err, "unsupported import mode")
		})
	})
}

func foreign() *Document {
	return &Document{
		Paths: map[string]*PathItem{
			"/x": {Get: &Operation{Responses: map[string]*Response{"200": {Description: "ok"}}}},
		},
	}
}
