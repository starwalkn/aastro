package aastro

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/starwalkn/aastro/sdk"
)

func TestRouter_ServeHTTP_SuccessfulFlow(t *testing.T) {
	t.Run("returns the aggregated array as the body, unwrapped", func(t *testing.T) {
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

		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Contains(t, res.Header.Get("Content-Type"), "application/json")
		assert.Empty(t, res.Header.Values("X-Partial-Errors"))
		jsonEqual(t, `["A","B"]`, body)
	})
}

func TestRouter_ServeHTTP_PartialResponse(t *testing.T) {
	t.Run("returns 206, the successful data unwrapped, and failures on X-Partial-Errors", func(t *testing.T) {
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

		assert.Equal(t, http.StatusPartialContent, res.StatusCode)
		assert.ElementsMatch(t, []string{ClientErrUpstreamUnavailable.String()}, res.Header.Values("X-Partial-Errors"))
		jsonEqual(t, `["A"]`, body)
	})
}

func TestRouter_ServeHTTP_AllUpstreamsFailing(t *testing.T) {
	t.Run("returns 502 as a Problem Details document", func(t *testing.T) {
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
		problem := decodeProblem(t, body)

		assert.Equal(t, http.StatusBadGateway, res.StatusCode)
		assert.Contains(t, res.Header.Get("Content-Type"), "application/problem+json")
		assert.Equal(t, http.StatusBadGateway, problem.Status)
		assert.Equal(t, "about:blank", problem.Type)
		assert.ElementsMatch(t, []ClientError{ClientErrUpstreamUnavailable}, problem.Errors)
	})
}

func TestRouter_ServeHTTP_MultipleDistinctUpstreamErrors(t *testing.T) {
	t.Run("lists every distinct error in the Problem Details Errors extension", func(t *testing.T) {
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
		problem := decodeProblem(t, body)

		assert.Equal(t, http.StatusBadGateway, res.StatusCode)
		assert.Len(t, problem.Errors, 2)
	})
}

func TestRouter_ServeHTTP_SingleUpstream(t *testing.T) {
	t.Run("forwards a successful response and its content-type as-is", func(t *testing.T) {
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

		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, "text/plain", res.Header.Get("Content-Type"))
		assert.Equal(t, "plain text", string(body))
	})

	t.Run("strips hop-by-hop headers, including TE, but keeps everything else", func(t *testing.T) {
		// Regression test: hopByHopHeaders keyed "TE" (not net/textproto's
		// canonical "Te") never matched, since http.Header always stores
		// and iterates canonical keys - the header leaked to the client.
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
			assert.Emptyf(t, res.Header.Get(h), "hop-by-hop header %q must not reach the client", h)
		}
		assert.Equal(t, "kept", res.Header.Get("X-Custom"))
	})

	t.Run("forwards a non-JSON client-error body verbatim, not wrapped in the envelope", func(t *testing.T) {
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

		assert.Equal(t, http.StatusNotFound, res.StatusCode)
		assert.Equal(t, "text/html", res.Header.Get("Content-Type"))
		assert.Equal(t, "<html>not found</html>", string(body))
	})

	t.Run("reports a gateway-side failure as a Problem Details response", func(t *testing.T) {
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
		problem := decodeProblem(t, body)

		assert.Equal(t, http.StatusBadGateway, res.StatusCode)
		assert.Contains(t, res.Header.Get("Content-Type"), "application/problem+json")
		assert.Equal(t, http.StatusBadGateway, problem.Status)
		assert.Equal(t, "about:blank", problem.Type)
		assert.ElementsMatch(t, []ClientError{ClientErrUpstreamUnavailable}, problem.Errors)
	})

	t.Run("returns 413 when the request body is too large", func(t *testing.T) {
		d := &mockScatter{tooLarge: true}

		r := newTestRouter([]flow{{
			path:      "/test/single-413",
			method:    http.MethodGet,
			upstreams: mockUpstreams("u"),
		}}, d, &defaultAggregator{})

		req := httptest.NewRequest(http.MethodGet, "/test/single-413", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	})
}

func TestRouter_ServeHTTP_NoFlowMatches(t *testing.T) {
	t.Run("returns 404", func(t *testing.T) {
		r := newTestRouter(nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/missing", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestRouter_ServeHTTP_Plugins(t *testing.T) {
	t.Run("runs request and response plugins in order", func(t *testing.T) {
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

		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, `"OK"`, string(body))
		assert.Equal(t, "done", res.Header.Get("X-Plugin"))
		assert.Equal(t, []string{"req", "resp"}, executed)
	})
}

func TestRouter_ServeHTTP_Middleware(t *testing.T) {
	t.Run("runs middleware before the handler", func(t *testing.T) {
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

		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, "ok", res.Header.Get("X-Middleware"))
	})
}

func TestRouter_ComputeFingerprint(t *testing.T) {
	t.Run("distinguishes header from query key with same name", func(t *testing.T) {
		r1 := httptest.NewRequest(http.MethodGet, "/u/{id}?id=1", nil)
		r1.Header.Set("Accept", "*/*")

		r2 := httptest.NewRequest(http.MethodGet, "/u/{id}", nil)
		r2.Header.Set("Accept", "*/*")
		r2.Header.Set("Id", "anything")

		assert.NotEqual(t, computeFingerprint(r2, "/u/{id}"), computeFingerprint(r1, "/u/{id}"))
	})
}
