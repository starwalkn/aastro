package openapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/voidrunner3074/aastro"
)

func minimalUpstream(name string) aastro.UpstreamConfig {
	return aastro.UpstreamConfig{
		Name:    name,
		Hosts:   aastro.AddrList{"http://" + name + ":8080"},
		Timeout: 3 * time.Second,
	}
}

func mergeFlow(path string, bestEffort bool, policy string, upstreams ...aastro.UpstreamConfig) aastro.FlowConfig {
	f := aastro.FlowConfig{
		Path:   path,
		Method: "GET",
		Aggregation: &aastro.AggregationConfig{
			Strategy:   "merge",
			BestEffort: bestEffort,
		},
		Upstreams: upstreams,
	}

	if policy != "" {
		f.Aggregation.OnConflict = &aastro.OnConflictConfig{Policy: policy}
	}

	return f
}

func streamingFlow(path string) aastro.FlowConfig {
	return aastro.FlowConfig{
		Path:      path,
		Method:    "GET",
		Streaming: true,
		Upstreams: []aastro.UpstreamConfig{minimalUpstream("stream")},
	}
}

func configWith(rateLimited bool, flows ...aastro.FlowConfig) aastro.Config {
	return aastro.Config{
		Schema: "v1",
		Gateway: aastro.GatewayConfig{
			Service: aastro.ServiceConfig{Name: "aastro"},
			Routing: aastro.RoutingConfig{
				RateLimiter: aastro.RateLimiterConfig{Enabled: rateLimited},
				Flows:       flows,
			},
		},
	}
}

func authMiddlewareConfig(cfg map[string]interface{}) aastro.MiddlewareConfig {
	return aastro.MiddlewareConfig{Name: "auth", Source: "builtin", Config: cfg}
}

func generate(t *testing.T, cfg aastro.Config, opts Options) (*Document, []Warning) {
	t.Helper()

	doc, warnings, err := FromConfig(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, doc)

	return doc, warnings
}

func TestFromConfig_DocumentSkeleton(t *testing.T) {
	t.Run("defaults info.title to the service name and version to 0.0.0", func(t *testing.T) {
		doc, _ := generate(t, configWith(false, mergeFlow("/a", false, "", minimalUpstream("u"))), Options{})

		assert.Equal(t, "3.1.0", doc.OpenAPI)
		assert.Equal(t, "aastro", doc.Info.Title)
		assert.Equal(t, "0.0.0", doc.Info.Version)
	})

	t.Run("honors title, api version, and servers overrides", func(t *testing.T) {
		doc, _ := generate(t,
			configWith(false, mergeFlow("/a", false, "", minimalUpstream("u"))),
			Options{Title: "My API", APIVersion: "1.2.3", Servers: []string{"https://api.example.com", "https://staging.example.com"}},
		)

		assert.Equal(t, "My API", doc.Info.Title)
		assert.Equal(t, "1.2.3", doc.Info.Version)
		assert.Equal(t, []Server{
			{URL: "https://api.example.com"},
			{URL: "https://staging.example.com"},
		}, doc.Servers)
	})

	t.Run("emits 3.0.3 when OASVersion is 3.0", func(t *testing.T) {
		doc, _ := generate(t, configWith(false, streamingFlow("/s")), Options{OASVersion: "3.0"})

		assert.Equal(t, "3.0.3", doc.OpenAPI)
	})

	t.Run("rejects unsupported OAS versions", func(t *testing.T) {
		_, _, err := FromConfig(configWith(false, streamingFlow("/s")), Options{OASVersion: "2.0"})

		assert.ErrorContains(t, err, "unsupported OpenAPI version")
	})

	t.Run("stamps the generator version into the root extension", func(t *testing.T) {
		doc, _ := generate(t, configWith(false, streamingFlow("/s")), Options{GeneratorVersion: "0.7.0"})

		require.NotNil(t, doc.XAastro)
		assert.Equal(t, "v1", doc.XAastro.Schema)
		assert.Equal(t, "aastroctl/0.7.0", doc.XAastro.Generator)
	})

	t.Run("derives tags from the first path segment", func(t *testing.T) {
		doc, _ := generate(t, configWith(false,
			mergeFlow("/api/v1/a", false, "", minimalUpstream("u")),
			mergeFlow("/internal/b", false, "", minimalUpstream("u")),
		), Options{})

		assert.Equal(t, []Tag{{Name: "api"}, {Name: "internal"}}, doc.Tags)
		assert.ElementsMatch(t, []string{"api"}, doc.Paths["/api/v1/a"].Get.Tags)
	})

	t.Run("is deterministic across invocations", func(t *testing.T) {
		cfg := configWith(true,
			mergeFlow("/a/{id}", true, "error", minimalUpstream("u1"), minimalUpstream("u2")),
			streamingFlow("/s"),
		)

		first, _ := generate(t, cfg, Options{Extensions: true})
		second, _ := generate(t, cfg, Options{Extensions: true})

		assert.Equal(t, first, second)
	})
}

func TestFromConfig_ResponseDerivation(t *testing.T) {
	t.Run("always includes the base envelope statuses for a multi-upstream flow", func(t *testing.T) {
		doc, _ := generate(t, configWith(false, mergeFlow("/a", false, "", minimalUpstream("u1"), minimalUpstream("u2"))), Options{})

		responses := doc.Paths["/a"].Get.Responses
		assert.Contains(t, responses, "200")
		assert.Contains(t, responses, "413")
		assert.Contains(t, responses, "500")
		assert.Contains(t, responses, "502")
		assert.Contains(t, responses["200"].Content, "application/json")
		assert.Nil(t, responses["200"].Content["application/json"].Schema)
		assert.Equal(t, schemaProblemDetails, responses["502"].Content["application/problem+json"].Schema.Ref)
	})

	t.Run("documents a single-upstream flow as an opaque proxy, not the envelope", func(t *testing.T) {
		doc, _ := generate(t, configWith(false, mergeFlow("/a", false, "", minimalUpstream("u"))), Options{})

		responses := doc.Paths["/a"].Get.Responses
		assert.Contains(t, responses, "200")
		assert.Contains(t, responses, "413")
		assert.Contains(t, responses, "500")
		assert.Contains(t, responses, "502")
		assert.NotContains(t, responses, "default")

		assert.Contains(t, responses["200"].Content, "*/*")
		assert.Nil(t, responses["200"].Content["*/*"].Schema)
		assert.Equal(t, schemaProblemDetails, responses["502"].Content["application/problem+json"].Schema.Ref)
	})

	t.Run("206 appears only for best-effort multi-upstream flows", func(t *testing.T) {
		tests := []struct {
			name          string
			bestEffort    bool
			upstreamCount int
			want          bool
		}{
			{"best-effort with two upstreams", true, 2, true},
			{"best-effort with one upstream", true, 1, false},
			{"strict with two upstreams", false, 2, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ups := make([]aastro.UpstreamConfig, 0, tt.upstreamCount)
				for range tt.upstreamCount {
					ups = append(ups, minimalUpstream("u"))
				}

				doc, _ := generate(t, configWith(false, mergeFlow("/a", tt.bestEffort, "", ups...)), Options{})

				if tt.want {
					assert.Contains(t, doc.Paths["/a"].Get.Responses, "206")
				} else {
					assert.NotContains(t, doc.Paths["/a"].Get.Responses, "206")
				}
			})
		}
	})

	t.Run("documents X-Partial-Errors only on the 206 response", func(t *testing.T) {
		doc, _ := generate(t, configWith(false, mergeFlow("/a", true, "", minimalUpstream("u1"), minimalUpstream("u2"))), Options{})

		responses := doc.Paths["/a"].Get.Responses
		assert.Contains(t, responses["206"].Headers, "X-Partial-Errors")
		assert.Nil(t, responses["206"].Content["application/json"].Schema)
		assert.NotContains(t, responses["200"].Headers, "X-Partial-Errors")
	})

	t.Run("409 appears only under on_conflict: error", func(t *testing.T) {
		tests := []struct {
			name   string
			policy string
			want   bool
		}{
			{"error policy", "error", true},
			{"prefer policy", "prefer", false},
			{"overwrite policy", "overwrite", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				doc, _ := generate(t,
					configWith(false, mergeFlow("/a", false, tt.policy, minimalUpstream("u1"), minimalUpstream("u2"))),
					Options{},
				)

				if tt.want {
					assert.Contains(t, doc.Paths["/a"].Get.Responses, "409")
				} else {
					assert.NotContains(t, doc.Paths["/a"].Get.Responses, "409")
				}
			})
		}
	})

	t.Run("ties 429 to the rate limiter for both flow kinds", func(t *testing.T) {
		limited, _ := generate(t, configWith(true, mergeFlow("/a", false, "", minimalUpstream("u")), streamingFlow("/s")), Options{})
		unlimited, _ := generate(t, configWith(false, mergeFlow("/a", false, "", minimalUpstream("u")), streamingFlow("/s")), Options{})

		assert.Contains(t, limited.Paths["/a"].Get.Responses, "429")
		assert.Contains(t, limited.Paths["/s"].Get.Responses, "429")
		assert.NotContains(t, unlimited.Paths["/a"].Get.Responses, "429")
		assert.NotContains(t, unlimited.Paths["/s"].Get.Responses, "429")
	})

	t.Run("models streaming as streamed */* without 413", func(t *testing.T) {
		doc, _ := generate(t, configWith(false, streamingFlow("/s")), Options{})

		responses := doc.Paths["/s"].Get.Responses
		assert.Contains(t, responses, "200")
		assert.Contains(t, responses, "502")
		assert.NotContains(t, responses, "413")
		assert.Contains(t, responses["200"].Content, "*/*")
		assert.Nil(t, responses["200"].Content["*/*"].Schema)
	})

	t.Run("adds a request body only for body-carrying methods", func(t *testing.T) {
		post := mergeFlow("/a", false, "", minimalUpstream("u"))
		post.Method = "POST"

		doc, _ := generate(t, configWith(false, post, mergeFlow("/b", false, "", minimalUpstream("u"))), Options{})

		assert.NotNil(t, doc.Paths["/a"].Post.RequestBody)
		assert.Nil(t, doc.Paths["/b"].Get.RequestBody)
	})

	t.Run("notes proxy semantics in the description for a single upstream", func(t *testing.T) {
		doc, _ := generate(t, configWith(false, mergeFlow("/a", false, "", minimalUpstream("u"))), Options{})

		assert.Contains(t, doc.Paths["/a"].Get.Description, "Proxy flow")
	})

	t.Run("describes the shared-status rule for multi-upstream flows", func(t *testing.T) {
		doc, _ := generate(t,
			configWith(false, mergeFlow("/a", false, "", minimalUpstream("u1"), minimalUpstream("u2"))),
			Options{},
		)

		assert.Contains(t, doc.Paths["/a"].Get.Responses["default"].Description, "every failing upstream")
	})
}

func TestFromConfig_AuthMiddlewareMapping(t *testing.T) {
	authFlow := func(path string, mwCfg map[string]interface{}) aastro.FlowConfig {
		f := mergeFlow(path, false, "", minimalUpstream("u"))
		f.Middlewares = []aastro.MiddlewareConfig{authMiddlewareConfig(mwCfg)}

		return f
	}

	t.Run("registers the bearer security scheme once and applies it per operation", func(t *testing.T) {
		doc, _ := generate(t, configWith(false,
			authFlow("/secured", map[string]interface{}{"issuer": "https://idp", "audience": "api"}),
			mergeFlow("/open", false, "", minimalUpstream("u")),
		), Options{})

		assert.Contains(t, doc.Components.SecuritySchemes, securitySchemeBearer)
		assert.Equal(t, "bearer", doc.Components.SecuritySchemes[securitySchemeBearer].Scheme)

		assert.Equal(t, []map[string][]string{{securitySchemeBearer: {}}}, doc.Paths["/secured"].Get.Security)
		assert.Contains(t, doc.Paths["/secured"].Get.Responses, "401")
		assert.Contains(t, doc.Paths["/secured"].Get.Description, "issued by `https://idp` for audience `api`")

		assert.Empty(t, doc.Paths["/open"].Get.Security)
		assert.NotContains(t, doc.Paths["/open"].Get.Responses, "401")
	})

	t.Run("models 401 with only the WWW-Authenticate header", func(t *testing.T) {
		doc, _ := generate(t, configWith(false, authFlow("/secured", nil)), Options{})

		resp := doc.Paths["/secured"].Get.Responses["401"]
		assert.Len(t, resp.Headers, 1)
		assert.Contains(t, resp.Headers, "WWW-Authenticate")
		assert.Equal(t, schemaProblemDetails, resp.Content["application/problem+json"].Schema.Ref)
	})

	t.Run("omits the security scheme when no flow uses auth", func(t *testing.T) {
		doc, _ := generate(t, configWith(false, mergeFlow("/open", false, "", minimalUpstream("u"))), Options{})

		assert.Empty(t, doc.Components.SecuritySchemes)
	})

	t.Run("ignores a file-sourced middleware named auth", func(t *testing.T) {
		f := mergeFlow("/a", false, "", minimalUpstream("u"))
		f.Middlewares = []aastro.MiddlewareConfig{{Name: "auth", Source: "file", Path: "/plugins/"}}

		doc, _ := generate(t, configWith(false, f), Options{})

		assert.Empty(t, doc.Paths["/a"].Get.Security)
		assert.NotContains(t, doc.Paths["/a"].Get.Responses, "401")
	})
}

func TestFromConfig_ParameterDerivation(t *testing.T) {
	t.Run("extracts path params in order and dedupes repeats", func(t *testing.T) {
		doc, _ := generate(t,
			configWith(false, mergeFlow("/a/{id}/b/{name}/c/{id}", false, "", minimalUpstream("u"))),
			Options{},
		)

		params := doc.Paths["/a/{id}/b/{name}/c/{id}"].Get.Parameters
		require.Len(t, params, 2)
		assert.Equal(t, Parameter{Name: "id", In: "path", Required: true, Schema: &Schema{Type: "string"}}, params[0])
		assert.Equal(t, "name", params[1].Name)
	})

	t.Run("unions forwarded queries and headers across upstreams, sorted", func(t *testing.T) {
		u1 := minimalUpstream("u1")
		u1.ForwardQueries = []string{"expand", "fields"}
		u1.ForwardHeaders = []string{"X-Tenant-Id"}

		u2 := minimalUpstream("u2")
		u2.ForwardQueries = []string{"expand", "limit"}
		u2.ForwardHeaders = []string{"Accept-Language"}

		doc, _ := generate(t, configWith(false, mergeFlow("/a", false, "", u1, u2)), Options{})

		var queries, headers []string

		for _, p := range doc.Paths["/a"].Get.Parameters {
			switch p.In {
			case "query":
				queries = append(queries, p.Name)
			case "header":
				headers = append(headers, p.Name)
			}
		}

		assert.Equal(t, []string{"expand", "fields", "limit"}, queries)
		assert.Equal(t, []string{"Accept-Language", "X-Tenant-Id"}, headers)
	})

	t.Run("drops Accept, Content-Type, and Authorization header params regardless of case", func(t *testing.T) {
		u := minimalUpstream("u")
		u.ForwardHeaders = []string{"authorization", "Content-Type", "ACCEPT", "X-Keep-Me"}

		doc, _ := generate(t, configWith(false, mergeFlow("/a", false, "", u)), Options{})

		var headers []string

		for _, p := range doc.Paths["/a"].Get.Parameters {
			if p.In == "header" {
				headers = append(headers, p.Name)
			}
		}

		assert.Equal(t, []string{"X-Keep-Me"}, headers)
	})

	t.Run("moves wildcards and prefix patterns into the description", func(t *testing.T) {
		u := minimalUpstream("u")
		u.ForwardQueries = []string{"*"}
		u.ForwardHeaders = []string{"X-Custom-*"}

		doc, _ := generate(t, configWith(false, mergeFlow("/a", false, "", u)), Options{})

		op := doc.Paths["/a"].Get
		assert.Empty(t, op.Parameters)
		assert.Contains(t, op.Description, "All query parameters are forwarded")
		assert.Contains(t, op.Description, "X-Custom-*")
	})
}

func TestFromConfig_QueryMethodAndWarnings(t *testing.T) {
	t.Run("emits QUERY flows under x-aastro-query with a warning", func(t *testing.T) {
		f := mergeFlow("/search", false, "", minimalUpstream("u"))
		f.Method = "QUERY"

		doc, warnings := generate(t, configWith(false, f), Options{})

		item := doc.Paths["/search"]
		assert.Nil(t, item.Get)
		require.NotNil(t, item.XAastroQuery)
		assert.NotNil(t, item.XAastroQuery.RequestBody)

		require.Len(t, warnings, 1)
		assert.Equal(t, "QUERY /search", warnings[0].Flow)
		assert.Contains(t, warnings[0].Message, "x-aastro-query")
	})

	t.Run("keeps the first operation and warns on duplicate method+path", func(t *testing.T) {
		first := mergeFlow("/dup", false, "", minimalUpstream("first"))
		second := mergeFlow("/dup", false, "", minimalUpstream("second"))

		doc, warnings := generate(t, configWith(false, first, second), Options{})

		assert.Contains(t, doc.Paths["/dup"].Get.Summary, "first")
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0].Message, "duplicate")
	})
}

func TestFromConfig_XAastroExtensions(t *testing.T) {
	t.Run("omits the flow extension unless enabled", func(t *testing.T) {
		doc, _ := generate(t, configWith(false, mergeFlow("/a", false, "", minimalUpstream("u"))), Options{})

		assert.Nil(t, doc.Paths["/a"].Get.XAastro)
	})

	t.Run("snapshots the flow but never middleware configs", func(t *testing.T) {
		f := mergeFlow("/a", true, "prefer", minimalUpstream("u1"), minimalUpstream("u2"))
		f.Aggregation.OnConflict.Upstream = "u1"
		f.Middlewares = []aastro.MiddlewareConfig{
			{Name: "recoverer", Source: "builtin"},
			authMiddlewareConfig(map[string]interface{}{
				"issuer":      "https://idp",
				"hmac_secret": "SECRET-MARKER-DO-NOT-LEAK",
			}),
		}

		doc, _ := generate(t, configWith(false, f), Options{Extensions: true})

		ext := doc.Paths["/a"].Get.XAastro
		require.NotNil(t, ext)
		assert.Equal(t, "merge", ext.Aggregation.Strategy)
		assert.True(t, ext.Aggregation.BestEffort)
		assert.Equal(t, "u1", ext.Aggregation.OnConflict.PreferUpstream)
		assert.Equal(t, []string{"recoverer", "auth"}, ext.Middlewares)
		assert.Len(t, ext.Upstreams, 2)
		assert.Equal(t, "3s", ext.Upstreams[0].Timeout)

		serialized, err := json.Marshal(doc)
		require.NoError(t, err)
		assert.NotContains(t, string(serialized), "SECRET-MARKER-DO-NOT-LEAK")
	})
}

func TestFromConfig_Components(t *testing.T) {
	t.Run("emits problem detail schemas even for streaming-only configs", func(t *testing.T) {
		doc, _ := generate(t, configWith(false, streamingFlow("/s")), Options{})

		require.NotNil(t, doc.Components)
		assert.Contains(t, doc.Components.Schemas, "ProblemDetails")
		assert.Contains(t, doc.Components.Schemas, "ClientError")
		assert.NotContains(t, doc.Components.Schemas, "ClientResponse")
	})
}

func TestOperationID(t *testing.T) {
	tests := []struct {
		name, method, path, want string
	}{
		{"path params flattened", "GET", "/api/v2/file/{id}", "get_api_v2_file_id"},
		{"hyphens sanitized", "GET", "/api/v1/health-check", "get_api_v1_health_check"},
		{"uppercase method lowered", "POST", "/a/B", "post_a_b"},
		{"root path", "GET", "/", "get"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, operationID(tt.method, tt.path))
		})
	}
}

func TestAuthNote(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]interface{}
		want string
	}{
		{
			"issuer and audience",
			map[string]interface{}{"issuer": "https://idp", "audience": "api"},
			"Requires a JWT issued by `https://idp` for audience `api`.",
		},
		{
			"issuer only",
			map[string]interface{}{"issuer": "https://idp"},
			"Requires a JWT issued by `https://idp`.",
		},
		{
			"audience only",
			map[string]interface{}{"audience": "api"},
			"Requires a JWT for audience `api`.",
		},
		{"neither", map[string]interface{}{}, ""},
		{"nil config", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, authNote(new(authMiddlewareConfig(tt.cfg))))
		})
	}
}
