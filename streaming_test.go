package aastro

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/voidrunner3074/aastro/sdk"
)

func TestStreaming_SSEResponse(t *testing.T) {
	t.Run("forwards body and headers without buffering", func(t *testing.T) {
		u := &mockProxyUpstream{
			upstreamName: "sse",
			proxyFn: func(w http.ResponseWriter, _ *http.Request) error {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, "data: hello\n\n")
				return nil
			},
		}

		r := newTestRouter([]flow{streamingFlow("/stream", u)}, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/stream", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)

		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, "text/event-stream", res.Header.Get("Content-Type"))
		assert.Equal(t, "no-cache", res.Header.Get("Cache-Control"))
		assert.Empty(t, res.Header.Get("Content-Length"))
		assert.Equal(t, "data: hello\n\n", string(body))
	})
}

func TestStreaming_MultipleSSEEvents(t *testing.T) {
	t.Run("forwards all events in order", func(t *testing.T) {
		events := "data: first\n\ndata: second\n\ndata: third\n\n"

		u := &mockProxyUpstream{
			upstreamName: "sse-multi",
			proxyFn: func(w http.ResponseWriter, _ *http.Request) error {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, events)
				return nil
			},
		}

		r := newTestRouter([]flow{streamingFlow("/events", u)}, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/events", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)

		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, events, string(body))
		assert.Equal(t, 3, strings.Count(string(body), "data:"))
	})
}

func TestStreaming_RequestPlugin(t *testing.T) {
	t.Run("runs the plugin before forwarding to upstream", func(t *testing.T) {
		var pluginSaw string

		plugin := &mockPlugin{
			name: "auth",
			typ:  sdk.PluginTypeRequest,
			fn: func(ctx sdk.Context) {
				ctx.Request().Header.Set("X-Auth", "injected")
			},
		}

		u := &mockProxyUpstream{
			upstreamName: "backend",
			proxyFn: func(w http.ResponseWriter, req *http.Request) error {
				pluginSaw = req.Header.Get("X-Auth")
				w.WriteHeader(http.StatusOK)
				return nil
			},
		}

		r := newTestRouter([]flow{streamingFlow("/plugin", u, plugin)}, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/plugin", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "injected", pluginSaw)
	})
}

func TestStreaming_UpstreamErrorsBeforeAnyWrite(t *testing.T) {
	t.Run("returns 502", func(t *testing.T) {
		u := &mockProxyUpstream{
			upstreamName: "broken",
			proxyFn: func(_ http.ResponseWriter, _ *http.Request) error {
				return errors.New("upstream connection refused")
			},
		}

		r := newTestRouter([]flow{streamingFlow("/broken", u)}, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/broken", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadGateway, rec.Code)
	})
}

func TestStreaming_UpstreamErrorsAfterPartialWrite(t *testing.T) {
	t.Run("does not panic and preserves the original status", func(t *testing.T) {
		u := &mockProxyUpstream{
			upstreamName: "partial",
			proxyFn: func(w http.ResponseWriter, _ *http.Request) error {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, "data: partial\n\n")
				return errors.New("connection reset by peer")
			},
		}

		r := newTestRouter([]flow{streamingFlow("/partial", u)}, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/partial", nil)
		rec := httptest.NewRecorder()

		assert.NotPanics(t, func() { r.ServeHTTP(rec, req) })
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestStreaming_NonProxyCapableUpstream(t *testing.T) {
	t.Run("returns 500", func(t *testing.T) {
		u := &mockUpstream{upstreamName: "plain"}

		r := newTestRouter([]flow{{
			path:      "/noproxy",
			method:    http.MethodGet,
			streaming: true,
			upstreams: []upstream{u},
		}}, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/noproxy", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestStreaming_MultipleUpstreams(t *testing.T) {
	t.Run("returns 500 because streaming requires exactly one", func(t *testing.T) {
		makeU := func(n string) upstream {
			return &mockProxyUpstream{
				upstreamName: n,
				proxyFn: func(w http.ResponseWriter, _ *http.Request) error {
					w.WriteHeader(http.StatusOK)
					return nil
				},
			}
		}

		r := newTestRouter([]flow{{
			path:      "/multi",
			method:    http.MethodGet,
			streaming: true,
			upstreams: []upstream{makeU("a"), makeU("b")},
		}}, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/multi", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestStreaming_NonOKUpstreamStatus(t *testing.T) {
	t.Run("preserves the status code", func(t *testing.T) {
		u := &mockProxyUpstream{
			upstreamName: "created",
			proxyFn: func(w http.ResponseWriter, _ *http.Request) error {
				w.WriteHeader(http.StatusCreated)
				return nil
			},
		}

		r := newTestRouter([]flow{streamingFlow("/created", u)}, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/created", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
	})
}
