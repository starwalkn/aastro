package openapi

import (
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/starwalkn/aastro"
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

func passthroughFlow(path string) aastro.FlowConfig {
	return aastro.FlowConfig{
		Path:        path,
		Method:      "GET",
		Passthrough: true,
		Upstreams:   []aastro.UpstreamConfig{minimalUpstream("stream")},
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

func generate(cfg aastro.Config, opts Options) (*Document, []Warning) {
	GinkgoHelper()

	doc, warnings, err := FromConfig(cfg, opts)
	Expect(err).NotTo(HaveOccurred())
	Expect(doc).NotTo(BeNil())

	return doc, warnings
}

var _ = Describe("FromConfig", func() {
	Describe("document skeleton", func() {
		It("defaults info.title to the service name and version to 0.0.0", func() {
			doc, _ := generate(configWith(false, mergeFlow("/a", false, "", minimalUpstream("u"))), Options{})

			Expect(doc.OpenAPI).To(Equal("3.1.0"))
			Expect(doc.Info.Title).To(Equal("aastro"))
			Expect(doc.Info.Version).To(Equal("0.0.0"))
		})

		It("honors title, api version, and servers overrides", func() {
			doc, _ := generate(
				configWith(false, mergeFlow("/a", false, "", minimalUpstream("u"))),
				Options{Title: "My API", APIVersion: "1.2.3", Servers: []string{"https://api.example.com", "https://staging.example.com"}},
			)

			Expect(doc.Info.Title).To(Equal("My API"))
			Expect(doc.Info.Version).To(Equal("1.2.3"))
			Expect(doc.Servers).To(HaveExactElements(
				Server{URL: "https://api.example.com"},
				Server{URL: "https://staging.example.com"},
			))
		})

		It("emits 3.0.3 when OASVersion is 3.0", func() {
			doc, _ := generate(configWith(false, passthroughFlow("/s")), Options{OASVersion: "3.0"})

			Expect(doc.OpenAPI).To(Equal("3.0.3"))
		})

		It("rejects unsupported OAS versions", func() {
			_, _, err := FromConfig(configWith(false, passthroughFlow("/s")), Options{OASVersion: "2.0"})

			Expect(err).To(MatchError(ContainSubstring("unsupported OpenAPI version")))
		})

		It("stamps the generator version into the root extension", func() {
			doc, _ := generate(configWith(false, passthroughFlow("/s")), Options{GeneratorVersion: "0.7.0"})

			Expect(doc.XAastro).NotTo(BeNil())
			Expect(doc.XAastro.Schema).To(Equal("v1"))
			Expect(doc.XAastro.Generator).To(Equal("aastroctl/0.7.0"))
		})

		It("derives tags from the first path segment", func() {
			doc, _ := generate(configWith(false,
				mergeFlow("/api/v1/a", false, "", minimalUpstream("u")),
				mergeFlow("/internal/b", false, "", minimalUpstream("u")),
			), Options{})

			Expect(doc.Tags).To(HaveExactElements(Tag{Name: "api"}, Tag{Name: "internal"}))
			Expect(doc.Paths["/api/v1/a"].Get.Tags).To(ConsistOf("api"))
		})

		It("is deterministic across invocations", func() {
			cfg := configWith(true,
				mergeFlow("/a/{id}", true, "error", minimalUpstream("u1"), minimalUpstream("u2")),
				passthroughFlow("/s"),
			)

			first, _ := generate(cfg, Options{Extensions: true})
			second, _ := generate(cfg, Options{Extensions: true})

			Expect(second).To(Equal(first))
		})
	})

	Describe("response derivation", func() {
		It("always includes the base envelope statuses", func() {
			doc, _ := generate(configWith(false, mergeFlow("/a", false, "", minimalUpstream("u"))), Options{})

			responses := doc.Paths["/a"].Get.Responses
			Expect(responses).To(HaveKey("200"))
			Expect(responses).To(HaveKey("413"))
			Expect(responses).To(HaveKey("500"))
			Expect(responses).To(HaveKey("502"))
			Expect(responses["200"].Content["application/json"].Schema.Ref).To(Equal(schemaClientResponse))
		})

		DescribeTable("206 appears only for best-effort multi-upstream flows",
			func(bestEffort bool, upstreamCount int, want bool) {
				ups := make([]aastro.UpstreamConfig, 0, upstreamCount)
				for range upstreamCount {
					ups = append(ups, minimalUpstream("u"))
				}

				doc, _ := generate(configWith(false, mergeFlow("/a", bestEffort, "", ups...)), Options{})

				if want {
					Expect(doc.Paths["/a"].Get.Responses).To(HaveKey("206"))
				} else {
					Expect(doc.Paths["/a"].Get.Responses).NotTo(HaveKey("206"))
				}
			},
			Entry("best-effort with two upstreams", true, 2, true),
			Entry("best-effort with one upstream", true, 1, false),
			Entry("strict with two upstreams", false, 2, false),
		)

		DescribeTable("409 appears only under on_conflict: error",
			func(policy string, want bool) {
				doc, _ := generate(
					configWith(false, mergeFlow("/a", false, policy, minimalUpstream("u1"), minimalUpstream("u2"))),
					Options{},
				)

				if want {
					Expect(doc.Paths["/a"].Get.Responses).To(HaveKey("409"))
				} else {
					Expect(doc.Paths["/a"].Get.Responses).NotTo(HaveKey("409"))
				}
			},
			Entry("error policy", "error", true),
			Entry("prefer policy", "prefer", false),
			Entry("overwrite policy", "overwrite", false),
		)

		It("ties 429 to the rate limiter for both flow kinds", func() {
			limited, _ := generate(configWith(true, mergeFlow("/a", false, "", minimalUpstream("u")), passthroughFlow("/s")), Options{})
			unlimited, _ := generate(configWith(false, mergeFlow("/a", false, "", minimalUpstream("u")), passthroughFlow("/s")), Options{})

			Expect(limited.Paths["/a"].Get.Responses).To(HaveKey("429"))
			Expect(limited.Paths["/s"].Get.Responses).To(HaveKey("429"))
			Expect(unlimited.Paths["/a"].Get.Responses).NotTo(HaveKey("429"))
			Expect(unlimited.Paths["/s"].Get.Responses).NotTo(HaveKey("429"))
		})

		It("models passthrough as streamed */* without 413", func() {
			doc, _ := generate(configWith(false, passthroughFlow("/s")), Options{})

			responses := doc.Paths["/s"].Get.Responses
			Expect(responses).To(HaveKey("200"))
			Expect(responses).To(HaveKey("502"))
			Expect(responses).NotTo(HaveKey("413"))
			Expect(responses["200"].Content).To(HaveKey("*/*"))
			Expect(responses["200"].Content["*/*"].Schema).To(BeNil())
		})

		It("adds a request body only for body-carrying methods", func() {
			post := mergeFlow("/a", false, "", minimalUpstream("u"))
			post.Method = "POST"

			doc, _ := generate(configWith(false, post, mergeFlow("/b", false, "", minimalUpstream("u"))), Options{})

			Expect(doc.Paths["/a"].Post.RequestBody).NotTo(BeNil())
			Expect(doc.Paths["/b"].Get.RequestBody).To(BeNil())
		})
	})

	Describe("auth middleware mapping", func() {
		authFlow := func(path string, mwCfg map[string]interface{}) aastro.FlowConfig {
			f := mergeFlow(path, false, "", minimalUpstream("u"))
			f.Middlewares = []aastro.MiddlewareConfig{authMiddlewareConfig(mwCfg)}

			return f
		}

		It("registers the bearer security scheme once and applies it per operation", func() {
			doc, _ := generate(configWith(false,
				authFlow("/secured", map[string]interface{}{"issuer": "https://idp", "audience": "api"}),
				mergeFlow("/open", false, "", minimalUpstream("u")),
			), Options{})

			Expect(doc.Components.SecuritySchemes).To(HaveKey(securitySchemeBearer))
			Expect(doc.Components.SecuritySchemes[securitySchemeBearer].Scheme).To(Equal("bearer"))

			Expect(doc.Paths["/secured"].Get.Security).To(HaveExactElements(map[string][]string{securitySchemeBearer: {}}))
			Expect(doc.Paths["/secured"].Get.Responses).To(HaveKey("401"))
			Expect(doc.Paths["/secured"].Get.Description).To(ContainSubstring("issued by `https://idp` for audience `api`"))

			Expect(doc.Paths["/open"].Get.Security).To(BeEmpty())
			Expect(doc.Paths["/open"].Get.Responses).NotTo(HaveKey("401"))
		})

		It("models 401 with only the WWW-Authenticate header", func() {
			doc, _ := generate(configWith(false, authFlow("/secured", nil)), Options{})

			resp := doc.Paths["/secured"].Get.Responses["401"]
			Expect(resp.Headers).To(HaveLen(1))
			Expect(resp.Headers).To(HaveKey("WWW-Authenticate"))
			Expect(resp.Content["application/json"].Schema.Ref).To(Equal(schemaClientResponse))
		})

		It("omits the security scheme when no flow uses auth", func() {
			doc, _ := generate(configWith(false, mergeFlow("/open", false, "", minimalUpstream("u"))), Options{})

			Expect(doc.Components.SecuritySchemes).To(BeEmpty())
		})

		It("ignores a file-sourced middleware named auth", func() {
			f := mergeFlow("/a", false, "", minimalUpstream("u"))
			f.Middlewares = []aastro.MiddlewareConfig{{Name: "auth", Source: "file", Path: "/plugins/"}}

			doc, _ := generate(configWith(false, f), Options{})

			Expect(doc.Paths["/a"].Get.Security).To(BeEmpty())
			Expect(doc.Paths["/a"].Get.Responses).NotTo(HaveKey("401"))
		})
	})

	Describe("parameter derivation", func() {
		It("extracts path params in order and dedupes repeats", func() {
			doc, _ := generate(
				configWith(false, mergeFlow("/a/{id}/b/{name}/c/{id}", false, "", minimalUpstream("u"))),
				Options{},
			)

			params := doc.Paths["/a/{id}/b/{name}/c/{id}"].Get.Parameters
			Expect(params).To(HaveLen(2))
			Expect(params[0]).To(Equal(Parameter{Name: "id", In: "path", Required: true, Schema: &Schema{Type: "string"}}))
			Expect(params[1].Name).To(Equal("name"))
		})

		It("unions forwarded queries and headers across upstreams, sorted", func() {
			u1 := minimalUpstream("u1")
			u1.ForwardQueries = []string{"expand", "fields"}
			u1.ForwardHeaders = []string{"X-Tenant-Id"}

			u2 := minimalUpstream("u2")
			u2.ForwardQueries = []string{"expand", "limit"}
			u2.ForwardHeaders = []string{"Accept-Language"}

			doc, _ := generate(configWith(false, mergeFlow("/a", false, "", u1, u2)), Options{})

			var queries, headers []string

			for _, p := range doc.Paths["/a"].Get.Parameters {
				switch p.In {
				case "query":
					queries = append(queries, p.Name)
				case "header":
					headers = append(headers, p.Name)
				}
			}

			Expect(queries).To(HaveExactElements("expand", "fields", "limit"))
			Expect(headers).To(HaveExactElements("Accept-Language", "X-Tenant-Id"))
		})

		It("drops Accept, Content-Type, and Authorization header params regardless of case", func() {
			u := minimalUpstream("u")
			u.ForwardHeaders = []string{"authorization", "Content-Type", "ACCEPT", "X-Keep-Me"}

			doc, _ := generate(configWith(false, mergeFlow("/a", false, "", u)), Options{})

			var headers []string

			for _, p := range doc.Paths["/a"].Get.Parameters {
				if p.In == "header" {
					headers = append(headers, p.Name)
				}
			}

			Expect(headers).To(HaveExactElements("X-Keep-Me"))
		})

		It("moves wildcards and prefix patterns into the description", func() {
			u := minimalUpstream("u")
			u.ForwardQueries = []string{"*"}
			u.ForwardHeaders = []string{"X-Custom-*"}

			doc, _ := generate(configWith(false, mergeFlow("/a", false, "", u)), Options{})

			op := doc.Paths["/a"].Get
			Expect(op.Parameters).To(BeEmpty())
			Expect(op.Description).To(ContainSubstring("All query parameters are forwarded"))
			Expect(op.Description).To(ContainSubstring("X-Custom-*"))
		})
	})

	Describe("QUERY method and warnings", func() {
		It("emits QUERY flows under x-aastro-query with a warning", func() {
			f := mergeFlow("/search", false, "", minimalUpstream("u"))
			f.Method = "QUERY"

			doc, warnings := generate(configWith(false, f), Options{})

			item := doc.Paths["/search"]
			Expect(item.Get).To(BeNil())
			Expect(item.XAastroQuery).NotTo(BeNil())
			Expect(item.XAastroQuery.RequestBody).NotTo(BeNil())

			Expect(warnings).To(HaveLen(1))
			Expect(warnings[0].Flow).To(Equal("QUERY /search"))
			Expect(warnings[0].Message).To(ContainSubstring("x-aastro-query"))
		})

		It("keeps the first operation and warns on duplicate method+path", func() {
			first := mergeFlow("/dup", false, "", minimalUpstream("first"))
			second := mergeFlow("/dup", false, "", minimalUpstream("second"))

			doc, warnings := generate(configWith(false, first, second), Options{})

			Expect(doc.Paths["/dup"].Get.Summary).To(ContainSubstring("first"))
			Expect(warnings).To(HaveLen(1))
			Expect(warnings[0].Message).To(ContainSubstring("duplicate"))
		})
	})

	Describe("x-aastro extensions", func() {
		It("omits the flow extension unless enabled", func() {
			doc, _ := generate(configWith(false, mergeFlow("/a", false, "", minimalUpstream("u"))), Options{})

			Expect(doc.Paths["/a"].Get.XAastro).To(BeNil())
		})

		It("snapshots the flow but never middleware configs", func() {
			f := mergeFlow("/a", true, "prefer", minimalUpstream("u1"), minimalUpstream("u2"))
			f.Aggregation.OnConflict.Upstream = "u1"
			f.Middlewares = []aastro.MiddlewareConfig{
				{Name: "recoverer", Source: "builtin"},
				authMiddlewareConfig(map[string]interface{}{
					"issuer":      "https://idp",
					"hmac_secret": "SECRET-MARKER-DO-NOT-LEAK",
				}),
			}

			doc, _ := generate(configWith(false, f), Options{Extensions: true})

			ext := doc.Paths["/a"].Get.XAastro
			Expect(ext).NotTo(BeNil())
			Expect(ext.Aggregation.Strategy).To(Equal("merge"))
			Expect(ext.Aggregation.BestEffort).To(BeTrue())
			Expect(ext.Aggregation.OnConflict.PreferUpstream).To(Equal("u1"))
			Expect(ext.Middlewares).To(HaveExactElements("recoverer", "auth"))
			Expect(ext.Upstreams).To(HaveLen(2))
			Expect(ext.Upstreams[0].Timeout).To(Equal("3s"))

			serialized, err := json.Marshal(doc)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(serialized)).NotTo(ContainSubstring("SECRET-MARKER-DO-NOT-LEAK"))
		})
	})

	Describe("components", func() {
		It("emits envelope schemas even for passthrough-only configs", func() {
			doc, _ := generate(configWith(false, passthroughFlow("/s")), Options{})

			Expect(doc.Components).NotTo(BeNil())
			Expect(doc.Components.Schemas).To(HaveKey("ClientResponse"))
			Expect(doc.Components.Schemas).To(HaveKey("ClientError"))
			Expect(doc.Components.Schemas).To(HaveKey("ResponseMeta"))
		})
	})
})

var _ = Describe("operationID", func() {
	DescribeTable("builds stable identifiers",
		func(method, path, want string) {
			Expect(operationID(method, path)).To(Equal(want))
		},
		Entry("path params flattened", "GET", "/api/v2/file/{id}", "get_api_v2_file_id"),
		Entry("hyphens sanitized", "GET", "/api/v1/health-check", "get_api_v1_health_check"),
		Entry("uppercase method lowered", "POST", "/a/B", "post_a_b"),
		Entry("root path", "GET", "/", "get"),
	)
})

var _ = Describe("authNote", func() {
	DescribeTable("describes token requirements from non-secret fields",
		func(cfg map[string]interface{}, want string) {
			m := authMiddlewareConfig(cfg)
			Expect(authNote(&m)).To(Equal(want))
		},
		Entry("issuer and audience", map[string]interface{}{"issuer": "https://idp", "audience": "api"},
			"Requires a JWT issued by `https://idp` for audience `api`."),
		Entry("issuer only", map[string]interface{}{"issuer": "https://idp"},
			"Requires a JWT issued by `https://idp`."),
		Entry("audience only", map[string]interface{}{"audience": "api"},
			"Requires a JWT for audience `api`."),
		Entry("neither", map[string]interface{}{}, ""),
		Entry("nil config", nil, ""),
	)
})
