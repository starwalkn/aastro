package aastro

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/voidrunner3074/aastro/internal/circuitbreaker"
)

func TestHTTPUpstream_ResolveHeaders(t *testing.T) {
	newTrustedProxyUpstream := func() *httpUpstream {
		return newTestUpstream("", withTrustedProxies(mustParseCIDR("10.0.0.0/8")))
	}

	t.Run("when proxy is untrusted", func(t *testing.T) {
		t.Run("sets X-Forwarded headers from RemoteAddr for HTTP", func(t *testing.T) {
			up := newTrustedProxyUpstream()

			orig, _ := http.NewRequest(http.MethodGet, "http://example.com/test", nil)
			orig.RemoteAddr = "1.2.3.4:12345"
			target, _ := http.NewRequest(orig.Method, orig.URL.String(), nil)

			up.resolveHeaders(target, orig, zap.NewNop())

			assert.Equal(t, "1.2.3.4", target.Header.Get("X-Forwarded-For"))
			assert.Equal(t, "http", target.Header.Get("X-Forwarded-Proto"))
			assert.Equal(t, "example.com", target.Header.Get("X-Forwarded-Host"))
			assert.Equal(t, "80", target.Header.Get("X-Forwarded-Port"))
			assert.Equal(t, "for=1.2.3.4; proto=http; host=example.com", target.Header.Get("Forwarded"))
		})

		t.Run("sets X-Forwarded headers from RemoteAddr for HTTPS", func(t *testing.T) {
			up := newTrustedProxyUpstream()

			orig, _ := http.NewRequest(http.MethodGet, "https://example.com:8443/test", nil)
			orig.RemoteAddr = "1.2.3.4:12345"
			orig.TLS = &tls.ConnectionState{}
			target, _ := http.NewRequest(orig.Method, orig.URL.String(), nil)

			up.resolveHeaders(target, orig, zap.NewNop())

			assert.Equal(t, "1.2.3.4", target.Header.Get("X-Forwarded-For"))
			assert.Equal(t, "https", target.Header.Get("X-Forwarded-Proto"))
			assert.Equal(t, "example.com:8443", target.Header.Get("X-Forwarded-Host"))
			assert.Equal(t, "8443", target.Header.Get("X-Forwarded-Port"))
		})

		t.Run("ignores incoming X-Forwarded-For from spoofed clients", func(t *testing.T) {
			up := newTrustedProxyUpstream()

			orig := requestWithClientIP(http.MethodGet, "http://example.com/test", "1.2.3.4:12345", "1.2.3.4")
			orig.Header.Set("X-Forwarded-For", "192.168.99.1")
			target, _ := http.NewRequest(orig.Method, orig.URL.String(), nil)

			up.resolveHeaders(target, orig, zap.NewNop())

			assert.Equal(t, "1.2.3.4", target.Header.Get("X-Forwarded-For"))
		})
	})

	t.Run("when proxy is trusted", func(t *testing.T) {
		t.Run("appends real client IP to existing X-Forwarded-For", func(t *testing.T) {
			up := newTrustedProxyUpstream()

			orig := requestWithClientIP(http.MethodGet, "http://example.com/test", "10.0.1.5:12345", "10.0.1.5")
			orig.Header.Set("X-Forwarded-For", "5.6.7.8")
			orig.Header.Set("X-Forwarded-Proto", "http")
			orig.Header.Set("X-Forwarded-Host", "example.com")
			orig.Header.Set("X-Forwarded-Port", "80")
			target, _ := http.NewRequest(orig.Method, orig.URL.String(), nil)

			up.resolveHeaders(target, orig, zap.NewNop())

			assert.Equal(t, "5.6.7.8, 10.0.1.5", target.Header.Get("X-Forwarded-For"))
			assert.Equal(t, "http", target.Header.Get("X-Forwarded-Proto"))
			assert.Equal(t, "example.com", target.Header.Get("X-Forwarded-Host"))
			assert.Equal(t, "80", target.Header.Get("X-Forwarded-Port"))
			assert.Equal(t, "for=10.0.1.5; proto=http; host=example.com", target.Header.Get("Forwarded"))
		})

		t.Run("falls back to derived proto when incoming proto is invalid", func(t *testing.T) {
			up := newTrustedProxyUpstream()

			orig, _ := http.NewRequest(http.MethodGet, "http://example.com/test", nil)
			orig.RemoteAddr = "10.0.1.6:12345"
			orig.Header.Set("X-Forwarded-Proto", "ftp")
			target, _ := http.NewRequest(orig.Method, orig.URL.String(), nil)

			up.resolveHeaders(target, orig, zap.NewNop())

			assert.Equal(t, "http", target.Header.Get("X-Forwarded-Proto"))
		})

		t.Run("falls back to default port when incoming port is invalid", func(t *testing.T) {
			up := newTrustedProxyUpstream()

			orig, _ := http.NewRequest(http.MethodGet, "http://example.com/test", nil)
			orig.RemoteAddr = "10.0.1.7:12345"
			orig.Header.Set("X-Forwarded-Port", "99999")
			target, _ := http.NewRequest(orig.Method, orig.URL.String(), nil)

			up.resolveHeaders(target, orig, zap.NewNop())

			assert.Equal(t, "80", target.Header.Get("X-Forwarded-Port"))
		})
	})
}

func TestHTTPUpstream_ResolveQueries(t *testing.T) {
	t.Run("forwards only configured query keys", func(t *testing.T) {
		up := &httpUpstream{
			cfg: upstreamConfig{forwardQueries: []string{"foo", "bar"}},
		}

		orig, _ := http.NewRequest(http.MethodGet, "http://example.com?foo=1&bar=2&baz=3", nil)
		target, _ := http.NewRequest(http.MethodGet, "http://upstream.com", nil)

		up.resolveQueries(target, orig)

		q := target.URL.Query()
		assert.Equal(t, "1", q.Get("foo"))
		assert.Equal(t, "2", q.Get("bar"))
		assert.False(t, q.Has("baz"))
	})

	t.Run("forwards all query keys when wildcard is configured", func(t *testing.T) {
		up := &httpUpstream{
			cfg: upstreamConfig{forwardQueries: []string{"*"}},
		}

		orig, _ := http.NewRequest(http.MethodGet, "http://example.com?a=1&b=2", nil)
		target, _ := http.NewRequest(http.MethodGet, "http://upstream.com", nil)

		up.resolveQueries(target, orig)

		q := target.URL.Query()
		assert.Equal(t, "1", q.Get("a"))
		assert.Equal(t, "2", q.Get("b"))
	})

	t.Run("forwards only configured path params", func(t *testing.T) {
		up := &httpUpstream{
			cfg: upstreamConfig{forwardParams: []string{"user_id"}},
		}

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("user_id", "42")
		rctx.URLParams.Add("order_id", "99")

		orig, _ := http.NewRequest(http.MethodGet, "http://example.com/users/42/orders/99", nil)
		orig = orig.WithContext(context.WithValue(orig.Context(), chi.RouteCtxKey, rctx))
		target, _ := http.NewRequest(http.MethodGet, "http://upstream.com", nil)

		up.resolveQueries(target, orig)

		q := target.URL.Query()
		assert.Equal(t, "42", q.Get("user_id"))
		assert.False(t, q.Has("order_id"))
	})

	t.Run("forwards all path params when wildcard is configured", func(t *testing.T) {
		up := &httpUpstream{
			cfg: upstreamConfig{forwardParams: []string{"*"}},
		}

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("user_id", "42")
		rctx.URLParams.Add("order_id", "99")

		orig, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
		orig = orig.WithContext(context.WithValue(orig.Context(), chi.RouteCtxKey, rctx))
		target, _ := http.NewRequest(http.MethodGet, "http://upstream.com", nil)

		up.resolveQueries(target, orig)

		q := target.URL.Query()
		assert.Equal(t, "42", q.Get("user_id"))
		assert.Equal(t, "99", q.Get("order_id"))
	})
}

func TestHTTPUpstream_FilterHeaders(t *testing.T) {
	t.Run("removes blacklisted headers and preserves the rest", func(t *testing.T) {
		up := &httpUpstream{
			cfg: upstreamConfig{
				policy: upstreamPolicy{
					headerBlacklist: map[string]struct{}{"X-Secret": {}},
				},
			},
		}

		headers := http.Header{
			"X-Secret":  []string{"sensitive"},
			"X-Forward": []string{"ok"},
		}

		result := up.filterHeaders(headers)

		assert.Empty(t, result.Get("X-Secret"))
		assert.Equal(t, "ok", result.Get("X-Forward"))
	})

	t.Run("clones all headers when blacklist is empty", func(t *testing.T) {
		up := &httpUpstream{cfg: upstreamConfig{policy: upstreamPolicy{}}}

		headers := http.Header{
			"X-One": []string{"1"},
			"X-Two": []string{"2"},
		}

		result := up.filterHeaders(headers)

		assert.Equal(t, "1", result.Get("X-One"))
		assert.Equal(t, "2", result.Get("X-Two"))
	})
}

func TestHTTPUpstream_ExpandPathParams(t *testing.T) {
	t.Run("replaces known params with their values", func(t *testing.T) {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "123")

		req, _ := http.NewRequest(http.MethodGet, "/items/123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		assert.Equal(t, "/items/123", expandPathParams("/items/{id}", req))
	})

	t.Run("preserves unknown params verbatim", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/items/123", nil)

		assert.Equal(t, "/items/{unknown}", expandPathParams("/items/{unknown}", req))
	})
}

func TestHTTPUpstream_ClassifyDoError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want upstreamErrorKind
	}{
		{"deadline exceeded → timeout", context.DeadlineExceeded, upstreamTimeout},
		{"canceled → canceled", context.Canceled, upstreamCanceled},
		{"EOF → connection error", io.EOF, upstreamConnection},
	}

	up := &httpUpstream{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, up.classifyDoError(tt.err))
		})
	}
}

func TestHTTPUpstream_SelectHost(t *testing.T) {
	t.Run("always returns 0 for a single host", func(t *testing.T) {
		up := &httpUpstream{
			cfg:     upstreamConfig{hosts: []string{"http://only"}},
			metrics: testMetrics,
			log:     zap.NewNop(),
		}

		for range 5 {
			assert.Equal(t, int64(0), up.selectHost(zap.NewNop()))
		}
	})

	t.Run("distributes evenly across hosts in round-robin mode", func(t *testing.T) {
		up := &httpUpstream{
			cfg: upstreamConfig{
				hosts:  []string{"a", "b", "c"},
				lbMode: lbModeRoundRobin,
			},
			metrics: testMetrics,
			log:     zap.NewNop(),
		}

		seen := make(map[int64]int)
		for range 6 {
			seen[up.selectHost(zap.NewNop())]++
		}

		for _, count := range seen {
			assert.Equal(t, 2, count)
		}
	})

	t.Run("prefers idle host in least-connections mode", func(t *testing.T) {
		up := &httpUpstream{
			cfg: upstreamConfig{
				hosts:  []string{"busy", "idle"},
				lbMode: lbModeLeastConns,
			},
			state: upstreamState{
				activeConnections: []int64{5, 0},
			},
			metrics: testMetrics,
			log:     zap.NewNop(),
		}

		assert.Equal(t, int64(1), up.selectHost(zap.NewNop()))
	})
}

func TestHTTPUpstream_Call(t *testing.T) {
	t.Run("returns successful response body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer server.Close()

		up := newTestUpstream(server.URL)
		resp := up.call(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil), nil)

		assert.Nil(t, resp.err)
		assert.Equal(t, `{"ok":true}`, string(resp.body))
	})

	t.Run("returns canceled error if context is already canceled", func(t *testing.T) {
		up := newTestUpstream("http://localhost:1")

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		resp := up.call(ctx, httptest.NewRequest(http.MethodGet, "/", nil), nil)

		require.NotNil(t, resp.err)
		assert.Equal(t, upstreamCanceled, resp.err.kind)
	})

	t.Run("aborts retry backoff when context is canceled", func(t *testing.T) {
		var calls atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		up := newTestUpstream(server.URL, withPolicy(upstreamPolicy{
			retry: retryPolicy{
				maxRetries:      5,
				retryOnStatuses: []int{http.StatusInternalServerError},
				backoffDelay:    500 * time.Millisecond,
			},
		}))

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		resp := up.call(ctx, httptest.NewRequest(http.MethodGet, "/", nil), nil)

		require.NotNil(t, resp.err)
		assert.Equal(t, upstreamCanceled, resp.err.kind)
		assert.LessOrEqual(t, calls.Load(), int32(2))
	})

	t.Run("respects circuit breaker after consecutive failures", func(t *testing.T) {
		var calls atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		cb := circuitbreaker.New(2, 100*time.Millisecond)
		up := newTestUpstream(server.URL, withCircuitBreaker(cb))

		for range 4 {
			up.call(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil), nil)
		}

		assert.Equal(t, int32(2), calls.Load())
	})

	t.Run("recovers after circuit breaker reset timeout", func(t *testing.T) {
		resetTimeout := 50 * time.Millisecond
		cb := circuitbreaker.New(1, resetTimeout)

		var calls atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		up := newTestUpstream(server.URL, withCircuitBreaker(cb))
		req := func() *http.Request { return httptest.NewRequest(http.MethodGet, "/", nil) }

		r1 := up.call(context.Background(), req(), nil)
		require.NotNil(t, r1.err)
		assert.Equal(t, upstreamBadStatus, r1.err.kind)

		r2 := up.call(context.Background(), req(), nil)
		require.NotNil(t, r2.err)
		assert.Equal(t, upstreamCircuitOpen, r2.err.kind)

		time.Sleep(resetTimeout + 20*time.Millisecond)

		r3 := up.call(context.Background(), req(), nil)
		assert.Nil(t, r3.err)
	})
}

func TestHTTPUpstream_TLS(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	startServer := func(t *testing.T, tlsConf *tls.Config) *httptest.Server {
		t.Helper()

		server := httptest.NewUnstartedServer(handler)
		server.TLS = tlsConf
		server.Config.ErrorLog = log.New(io.Discard, "", 0)
		server.StartTLS()
		t.Cleanup(server.Close)

		return server
	}

	doRequest := func(up *httpUpstream) *upstreamResponse {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		return up.call(context.Background(), req, nil)
	}

	t.Run("when server presents a cert signed by a trusted CA completes the request successfully", func(t *testing.T) {
		fx := newTLSFixture(t)
		server := startServer(t, &tls.Config{
			Certificates: []tls.Certificate{fx.issueServerCert(t)},
		})

		up := newTestUpstream(server.URL, withTLS(&tls.Config{
			RootCAs: fx.certPool(),
		}))

		resp := doRequest(up)

		assert.Nil(t, resp.err)
		assert.Equal(t, http.StatusOK, resp.status)
		assert.Equal(t, `{"ok":true}`, string(resp.body))
	})

	t.Run("when server presents a cert signed by an untrusted CA fails the handshake with a connection error", func(t *testing.T) {
		fx := newTLSFixture(t)
		server := startServer(t, &tls.Config{
			Certificates: []tls.Certificate{fx.issueServerCert(t)},
		})

		up := newTestUpstream(server.URL, withTLS(&tls.Config{
			RootCAs: x509.NewCertPool(),
		}))

		resp := doRequest(up)

		require.NotNil(t, resp.err)
		assert.Equal(t, upstreamConnection, resp.err.kind)
	})

	t.Run("when the client opts into InsecureSkipVerify succeeds even without a trusted CA in the pool", func(t *testing.T) {
		fx := newTLSFixture(t)
		server := startServer(t, &tls.Config{
			Certificates: []tls.Certificate{fx.issueServerCert(t)},
		})

		up := newTestUpstream(server.URL, withTLS(&tls.Config{
			InsecureSkipVerify: true,
		}))

		resp := doRequest(up)

		assert.Nil(t, resp.err)
		assert.Equal(t, http.StatusOK, resp.status)
	})

	t.Run("when server requires mTLS and client provides a valid cert completes the handshake and returns 200", func(t *testing.T) {
		fx := newTLSFixture(t)
		server := startServer(t, &tls.Config{
			Certificates: []tls.Certificate{fx.issueServerCert(t)},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    fx.certPool(),
		})

		up := newTestUpstream(server.URL, withTLS(&tls.Config{
			RootCAs:      fx.certPool(),
			Certificates: []tls.Certificate{fx.issueClientCert(t, "aastro")},
		}))

		resp := doRequest(up)

		assert.Nil(t, resp.err)
		assert.Equal(t, http.StatusOK, resp.status)
	})

	t.Run("when server requires mTLS but client omits its cert is rejected on handshake", func(t *testing.T) {
		fx := newTLSFixture(t)
		server := startServer(t, &tls.Config{
			Certificates: []tls.Certificate{fx.issueServerCert(t)},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    fx.certPool(),
		})

		up := newTestUpstream(server.URL, withTLS(&tls.Config{
			RootCAs: fx.certPool(),
		}))

		resp := doRequest(up)

		require.NotNil(t, resp.err)
		assert.Equal(t, upstreamConnection, resp.err.kind)
	})

	t.Run("when server requires mTLS but client presents a cert from a foreign CA is rejected on handshake", func(t *testing.T) {
		fx := newTLSFixture(t)
		rogue := newTLSFixture(t)

		server := startServer(t, &tls.Config{
			Certificates: []tls.Certificate{fx.issueServerCert(t)},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    fx.certPool(),
		})

		up := newTestUpstream(server.URL, withTLS(&tls.Config{
			RootCAs:      fx.certPool(),
			Certificates: []tls.Certificate{rogue.issueClientCert(t, "rogue")},
		}))

		resp := doRequest(up)

		require.NotNil(t, resp.err)
		assert.Equal(t, upstreamConnection, resp.err.kind)
	})

	t.Run("when client requires TLS 1.3 but server caps at 1.2 fails with a version mismatch", func(t *testing.T) {
		fx := newTLSFixture(t)
		server := startServer(t, &tls.Config{
			Certificates: []tls.Certificate{fx.issueServerCert(t)},
			MaxVersion:   tls.VersionTLS12,
		})

		up := newTestUpstream(server.URL, withTLS(&tls.Config{
			RootCAs:    fx.certPool(),
			MinVersion: tls.VersionTLS13,
		}))

		resp := doRequest(up)

		require.NotNil(t, resp.err)
		assert.Equal(t, upstreamConnection, resp.err.kind)
	})
}
