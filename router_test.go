package aastro

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/starwalkn/aastro/sdk"
)

var _ = Describe("Router", func() {
	Describe("ServeHTTP", func() {
		Context("with a successful flow", func() {
			It("returns the aggregated array as the body, unwrapped", func() {
				d := &mockScatter{
					results: []upstreamResponse{
						{status: http.StatusOK, body: []byte(`"A"`), err: nil},
						{status: http.StatusOK, body: []byte(`"B"`), err: nil},
					},
				}

				r := newTestRouter([]flow{{
					path:   "/test/basic",
					method: http.MethodGet,
					aggregation: aggregation{
						strategy:   strategyArray,
						bestEffort: false,
					},
				}}, d, &defaultAggregator{})

				req := httptest.NewRequest(http.MethodGet, "/test/basic", nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)

				res := rec.Result()
				defer res.Body.Close()
				body, _ := io.ReadAll(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(res.Header.Get("Content-Type")).To(ContainSubstring("application/json"))
				Expect(res.Header.Values("X-Partial-Errors")).To(BeEmpty())
				jsonEqual(`["A","B"]`, body)
			})
		})

		Context("with a partial response", func() {
			It("returns 206, the successful data unwrapped, and failures on X-Partial-Errors", func() {
				d := &mockScatter{
					results: []upstreamResponse{
						{status: http.StatusOK, body: []byte(`"A"`), err: nil},
						{status: http.StatusInternalServerError, body: nil, err: &upstreamError{
							kind: upstreamTimeout,
							err:  errors.New("upstream timeout"),
						}},
					},
				}

				r := newTestRouter([]flow{{
					path:   "/test/partial",
					method: http.MethodGet,
					aggregation: aggregation{
						strategy:   strategyArray,
						bestEffort: true,
					},
				}}, d, &defaultAggregator{})

				req := httptest.NewRequest(http.MethodGet, "/test/partial", nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)

				res := rec.Result()
				defer res.Body.Close()
				body, _ := io.ReadAll(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusPartialContent))
				Expect(res.Header.Values("X-Partial-Errors")).To(ConsistOf(ClientErrUpstreamUnavailable.String()))
				jsonEqual(`["A"]`, body)
			})
		})

		Context("with all upstreams failing", func() {
			It("returns 502 as a Problem Details document", func() {
				d := &mockScatter{
					results: []upstreamResponse{
						{status: http.StatusOK, body: []byte(`"A"`), err: nil},
						{status: http.StatusInternalServerError, err: &upstreamError{
							kind: upstreamTimeout,
							err:  errors.New("upstream timeout"),
						}},
					},
				}

				r := newTestRouter([]flow{{
					path:   "/test/error",
					method: http.MethodGet,
					aggregation: aggregation{
						strategy:   strategyArray,
						bestEffort: false,
					},
				}}, d, &defaultAggregator{})

				req := httptest.NewRequest(http.MethodGet, "/test/error", nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)

				res := rec.Result()
				defer res.Body.Close()
				body, _ := io.ReadAll(res.Body)
				problem := decodeProblem(body)

				Expect(res.StatusCode).To(Equal(http.StatusBadGateway))
				Expect(res.Header.Get("Content-Type")).To(ContainSubstring("application/problem+json"))
				Expect(problem.Status).To(Equal(http.StatusBadGateway))
				Expect(problem.Type).To(Equal("about:blank"))
				Expect(problem.Errors).To(ConsistOf(ClientErrUpstreamUnavailable))
			})
		})

		Context("with multiple distinct upstream errors", func() {
			It("lists every distinct error in the Problem Details Errors extension", func() {
				d := &mockScatter{
					results: []upstreamResponse{
						{err: &upstreamError{kind: "unknown_error_kind", err: errors.New("unknown")}},
						{err: &upstreamError{kind: upstreamTimeout, err: errors.New("timeout")}},
					},
				}

				r := newTestRouter([]flow{{
					path:   "/test/priority",
					method: http.MethodGet,
					aggregation: aggregation{
						strategy:   strategyArray,
						bestEffort: true,
					},
				}}, d, &defaultAggregator{})

				req := httptest.NewRequest(http.MethodGet, "/test/priority", nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)

				res := rec.Result()
				defer res.Body.Close()
				body, _ := io.ReadAll(res.Body)
				problem := decodeProblem(body)

				Expect(res.StatusCode).To(Equal(http.StatusBadGateway))
				Expect(problem.Errors).To(HaveLen(2))
			})
		})

		Context("with a single upstream", func() {
			It("forwards a successful response and its content-type as-is", func() {
				d := &mockScatter{
					results: []upstreamResponse{
						{status: http.StatusOK, body: []byte("plain text"), headers: http.Header{"Content-Type": {"text/plain"}}},
					},
				}

				r := newTestRouter([]flow{{
					path:      "/test/single",
					method:    http.MethodGet,
					upstreams: mockUpstreams("u"),
				}}, d, &defaultAggregator{})

				req := httptest.NewRequest(http.MethodGet, "/test/single", nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)

				res := rec.Result()
				defer res.Body.Close()
				body, _ := io.ReadAll(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(res.Header.Get("Content-Type")).To(Equal("text/plain"))
				Expect(string(body)).To(Equal("plain text"))
			})

			It("strips hop-by-hop headers, including TE, but keeps everything else", func() {
				// Regression test: hopByHopHeaders keyed "TE" (not net/textproto's
				// canonical "Te") never matched, since http.Header always stores
				// and iterates canonical keys — the header leaked to the client.
				d := &mockScatter{
					results: []upstreamResponse{
						{
							status: http.StatusOK,
							body:   []byte("ok"),
							headers: http.Header{
								"Connection":         {"keep-alive"},
								"Te":                 {"trailers"},
								"Trailer":            {"X-Trailer"},
								"Keep-Alive":         {"timeout=5"},
								"Proxy-Authenticate": {"Basic"},
								"Upgrade":            {"h2c"},
								"X-Custom":           {"kept"},
							},
						},
					},
				}

				r := newTestRouter([]flow{{
					path:      "/test/hop-by-hop",
					method:    http.MethodGet,
					upstreams: mockUpstreams("u"),
				}}, d, &defaultAggregator{})

				req := httptest.NewRequest(http.MethodGet, "/test/hop-by-hop", nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)

				res := rec.Result()
				defer res.Body.Close()

				for _, h := range []string{"Connection", "Te", "Trailer", "Keep-Alive", "Proxy-Authenticate", "Upgrade"} {
					Expect(res.Header.Get(h)).To(BeEmpty(), "hop-by-hop header %q must not reach the client", h)
				}
				Expect(res.Header.Get("X-Custom")).To(Equal("kept"))
			})

			It("forwards a non-JSON client-error body verbatim, not wrapped in the envelope", func() {
				d := &mockScatter{
					results: []upstreamResponse{
						{
							status:  http.StatusNotFound,
							body:    []byte("<html>not found</html>"),
							headers: http.Header{"Content-Type": {"text/html"}},
							err:     &upstreamError{kind: upstreamClientError, err: errors.New("upstream returned 404")},
						},
					},
				}

				r := newTestRouter([]flow{{
					path:      "/test/single-404",
					method:    http.MethodGet,
					upstreams: mockUpstreams("u"),
				}}, d, &defaultAggregator{})

				req := httptest.NewRequest(http.MethodGet, "/test/single-404", nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)

				res := rec.Result()
				defer res.Body.Close()
				body, _ := io.ReadAll(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(res.Header.Get("Content-Type")).To(Equal("text/html"))
				Expect(string(body)).To(Equal("<html>not found</html>"))
			})

			It("reports a gateway-side failure as a Problem Details response", func() {
				d := &mockScatter{
					results: []upstreamResponse{
						{err: &upstreamError{kind: upstreamTimeout, err: errors.New("upstream timeout")}},
					},
				}

				r := newTestRouter([]flow{{
					path:      "/test/single-timeout",
					method:    http.MethodGet,
					upstreams: mockUpstreams("u"),
				}}, d, &defaultAggregator{})

				req := httptest.NewRequest(http.MethodGet, "/test/single-timeout", nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)

				res := rec.Result()
				defer res.Body.Close()
				body, _ := io.ReadAll(res.Body)
				problem := decodeProblem(body)

				Expect(res.StatusCode).To(Equal(http.StatusBadGateway))
				Expect(res.Header.Get("Content-Type")).To(ContainSubstring("application/problem+json"))
				Expect(problem.Status).To(Equal(http.StatusBadGateway))
				Expect(problem.Type).To(Equal("about:blank"))
				Expect(problem.Errors).To(ConsistOf(ClientErrUpstreamUnavailable))
			})

			It("returns 413 when the request body is too large", func() {
				d := &mockScatter{tooLarge: true}

				r := newTestRouter([]flow{{
					path:      "/test/single-413",
					method:    http.MethodGet,
					upstreams: mockUpstreams("u"),
				}}, d, &defaultAggregator{})

				req := httptest.NewRequest(http.MethodGet, "/test/single-413", nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)

				Expect(rec.Code).To(Equal(http.StatusRequestEntityTooLarge))
			})
		})

		Context("when no flow matches", func() {
			It("returns 404", func() {
				r := newTestRouter(nil, nil, nil)

				req := httptest.NewRequest(http.MethodGet, "/missing", nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)

				Expect(rec.Code).To(Equal(http.StatusNotFound))
			})
		})

		Context("with plugins", func() {
			It("runs request and response plugins in order", func() {
				var executed []string

				requestPlugin := &mockPlugin{
					name: "req",
					typ:  sdk.PluginTypeRequest,
					fn: func(_ sdk.Context) {
						executed = append(executed, "req")
					},
				}
				responsePlugin := &mockPlugin{
					name: "resp",
					typ:  sdk.PluginTypeResponse,
					fn: func(ctx sdk.Context) {
						executed = append(executed, "resp")
						ctx.Response().Header.Set("X-Plugin", "done")
					},
				}

				d := &mockScatter{
					results: []upstreamResponse{
						{status: http.StatusOK, body: []byte(`"OK"`)},
					},
				}

				r := newTestRouter([]flow{{
					path:      "/test/plugins",
					method:    http.MethodGet,
					plugins:   []sdk.Plugin{requestPlugin, responsePlugin},
					upstreams: mockUpstreams("u"),
				}}, d, &defaultAggregator{})

				req := httptest.NewRequest(http.MethodGet, "/test/plugins", nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)

				res := rec.Result()
				defer res.Body.Close()
				body, _ := io.ReadAll(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(string(body)).To(Equal(`"OK"`))
				Expect(res.Header.Get("X-Plugin")).To(Equal("done"))
				Expect(executed).To(Equal([]string{"req", "resp"}))
			})
		})

		Context("with middleware", func() {
			It("runs middleware before the handler", func() {
				d := &mockScatter{
					results: []upstreamResponse{
						{status: http.StatusOK, body: []byte(`"OK"`)},
					},
				}

				r := newTestRouter([]flow{{
					path:        "/test/mw",
					method:      http.MethodGet,
					middlewares: []sdk.Middleware{&mockMiddleware{}},
					upstreams:   mockUpstreams("u"),
				}}, d, &defaultAggregator{})

				req := httptest.NewRequest(http.MethodGet, "/test/mw", nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)

				res := rec.Result()
				defer res.Body.Close()

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(res.Header.Get("X-Middleware")).To(Equal("ok"))
			})
		})
	})

	Describe("computeFingerprint", func() {
		It("distinguishes header from query key with same name", func() {
			r1 := httptest.NewRequest(http.MethodGet, "/u/{id}?id=1", nil)
			r1.Header.Set("Accept", "*/*")

			r2 := httptest.NewRequest(http.MethodGet, "/u/{id}", nil)
			r2.Header.Set("Accept", "*/*")
			r2.Header.Set("Id", "anything")

			Expect(computeFingerprint(r1, "/u/{id}")).
				ToNot(Equal(computeFingerprint(r2, "/u/{id}")))
		})
	})
})
