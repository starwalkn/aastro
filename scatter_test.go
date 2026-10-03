package aastro

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/voidrunner3074/aastro/internal/circuitbreaker"
)

func TestScatter_DispatchingToMultipleUpstreams(t *testing.T) {
	t.Run("returns responses from all of them", func(t *testing.T) {
		serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte("A"))
		}))
		defer serverA.Close()

		serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte("B"))
		}))
		defer serverB.Close()

		f := newTestFlow([]upstream{
			newTestUpstream(serverA.URL),
			newTestUpstream(serverB.URL),
		})

		results := newTestScatter().scatter(f, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Len(t, results, 2)
		assert.Nil(t, results[0].err)
		assert.Nil(t, results[1].err)
		assert.Equal(t, "A", string(results[0].body))
		assert.Equal(t, "B", string(results[1].body))
	})
}

func TestScatter_Call(t *testing.T) {
	t.Run("calls the single upstream directly, without a result slice", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("solo"))
		}))
		defer server.Close()

		f := newTestFlow([]upstream{newTestUpstream(server.URL)})

		resp, ok := newTestScatter().call(f, httptest.NewRequest(http.MethodGet, "/", nil))

		assert.True(t, ok)
		assert.Nil(t, resp.err)
		assert.Equal(t, "solo", string(resp.body))
	})

	t.Run("returns ok=false for an oversized request body", func(t *testing.T) {
		f := newTestFlow([]upstream{newTestUpstream("http://localhost")})

		oversizedBody := bytes.Repeat([]byte("x"), maxBodySize+1)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(oversizedBody))

		_, ok := newTestScatter().call(f, req)

		assert.False(t, ok)
	})
}

func TestScatter_ForwardingRequestData(t *testing.T) {
	t.Run("forwards POST body to upstream", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_, _ = w.Write(body)
		}))
		defer server.Close()

		f := newTestFlow([]upstream{
			newTestUpstream(server.URL, withMethod(http.MethodPost)),
		})

		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("hello"))
		results := newTestScatter().scatter(f, req)

		require.Len(t, results, 1)
		assert.Nil(t, results[0].err)
		assert.Equal(t, "hello", string(results[0].body))
	})

	t.Run("returns nil for an oversized request body", func(t *testing.T) {
		f := newTestFlow([]upstream{
			newTestUpstream("http://localhost"),
		})

		oversizedBody := bytes.Repeat([]byte("x"), maxBodySize+1)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(oversizedBody))

		assert.Nil(t, newTestScatter().scatter(f, req))
	})

	t.Run("forwards configured query and header values", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query().Get("foo")
			h := r.Header.Get("X-Test")
			_, _ = w.Write([]byte(q + "-" + h))
		}))
		defer server.Close()

		f := newTestFlow([]upstream{
			newTestUpstream(server.URL,
				withForwardQueries("foo"),
				withForwardHeaders("X-Test"),
			),
		})

		req := httptest.NewRequest(http.MethodGet, "/?foo=bar", nil)
		req.Header.Set("X-Test", "baz")
		results := newTestScatter().scatter(f, req)

		require.Len(t, results, 1)
		assert.Equal(t, "bar-baz", string(results[0].body))
	})

	t.Run("forwards path params as query when configured", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(r.URL.Query().Get("user_id")))
		}))
		defer server.Close()

		f := newTestFlow([]upstream{
			newTestUpstream(server.URL, withForwardParams("user_id")),
		})

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("user_id", "42")
		req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		results := newTestScatter().scatter(f, req)

		require.Len(t, results, 1)
		assert.Equal(t, "42", string(results[0].body))
	})

	t.Run("expands path params in upstream path template", func(t *testing.T) {
		var receivedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		f := newTestFlow([]upstream{
			newTestUpstream(server.URL, withPath("/orders/{order_id}")),
		})

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("order_id", "99")
		req := httptest.NewRequest(http.MethodGet, "/orders/99", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		newTestScatter().scatter(f, req)

		assert.Equal(t, "/orders/99", receivedPath)
	})
}

func TestScatter_UpstreamPolicies(t *testing.T) {
	t.Run("rejects empty body when require_body is set", func(t *testing.T) {
		serverWithBody := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer serverWithBody.Close()

		serverNoBody := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer serverNoBody.Close()

		f := newTestFlow([]upstream{
			newTestUpstream(serverWithBody.URL, withPolicy(upstreamPolicy{requireBody: true})),
			newTestUpstream(serverNoBody.URL, withPolicy(upstreamPolicy{requireBody: true})),
		})

		results := newTestScatter().scatter(f, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Len(t, results, 2)
		assert.Nil(t, results[0].err)
		require.NotNil(t, results[1].err)
		assert.EqualError(t, results[1].err.Unwrap(), "empty body not allowed by upstream policy")
	})

	t.Run("rejects responses larger than max_response_body_size", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("abcdefghijklmnopqrstuvwxyz"))
		}))
		defer server.Close()

		f := newTestFlow([]upstream{
			newTestUpstream(server.URL, withPolicy(upstreamPolicy{maxResponseBodySize: 10})),
		})

		results := newTestScatter().scatter(f, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Len(t, results, 1)
		require.NotNil(t, results[0].err)
		assert.Equal(t, upstreamBodyTooLarge, results[0].err.kind)
	})

	t.Run("classifies upstream timeouts", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(600 * time.Millisecond)
		}))
		defer server.Close()

		f := newTestFlow([]upstream{
			newTestUpstream(server.URL, withTimeout(100*time.Millisecond)),
		})

		results := newTestScatter().scatter(f, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Len(t, results, 1)
		require.NotNil(t, results[0].err)
		assert.Equal(t, upstreamTimeout, results[0].err.kind)
	})
}

func TestScatter_RetryPolicy(t *testing.T) {
	t.Run("succeeds after a few transient failures", func(t *testing.T) {
		var attempts atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if attempts.Add(1) <= 2 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		f := newTestFlow([]upstream{
			newTestUpstream(server.URL, withPolicy(upstreamPolicy{
				retry: retryPolicy{
					maxRetries:      3,
					retryOnStatuses: []int{http.StatusInternalServerError},
					backoffDelay:    10 * time.Millisecond,
				},
			})),
		})

		results := newTestScatter().scatter(f, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Len(t, results, 1)
		assert.Nil(t, results[0].err)
		assert.Equal(t, int32(3), attempts.Load())
	})

	t.Run("still retries when the upstream has no explicit method (falls back to the request's)", func(t *testing.T) {
		// Regression test: shouldRetry must judge idempotency against the
		// effective method (config method, falling back to the original
		// request's), not the raw config value - an unset upstream method
		// used to make isIdempotent("") always false, silently disabling
		// retries for the common case of not overriding the method.
		var attempts atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if attempts.Add(1) <= 2 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		f := newTestFlow([]upstream{
			newTestUpstream(server.URL, withMethod(""), withPolicy(upstreamPolicy{
				retry: retryPolicy{
					maxRetries:      3,
					retryOnStatuses: []int{http.StatusInternalServerError},
					backoffDelay:    10 * time.Millisecond,
				},
			})),
		})

		results := newTestScatter().scatter(f, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Len(t, results, 1)
		assert.Nil(t, results[0].err)
		assert.Equal(t, int32(3), attempts.Load())
	})

	t.Run("gives up after exhausting max retries", func(t *testing.T) {
		var attempts atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		maxRetries := 3
		f := newTestFlow([]upstream{
			newTestUpstream(server.URL, withPolicy(upstreamPolicy{
				retry: retryPolicy{
					maxRetries:      maxRetries,
					retryOnStatuses: []int{http.StatusInternalServerError},
					backoffDelay:    10 * time.Millisecond,
				},
			})),
		})

		results := newTestScatter().scatter(f, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Len(t, results, 1)
		require.NotNil(t, results[0].err)
		assert.Equal(t, upstreamBadStatus, results[0].err.kind)
		assert.Equal(t, int32(maxRetries+1), attempts.Load())
	})
}

func TestScatter_CircuitBreaker(t *testing.T) {
	t.Run("opens after configured failure count and rejects further requests", func(t *testing.T) {
		var upstreamCalls atomic.Int32

		maxFailures := 3
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			upstreamCalls.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		cb := circuitbreaker.New(maxFailures, 100*time.Millisecond)
		f := newTestFlow([]upstream{
			newTestUpstream(server.URL, withCircuitBreaker(cb)),
		})

		d := newTestScatter()
		results := make([]upstreamResponse, 5)
		for i := range results {
			responses := d.scatter(f, httptest.NewRequest(http.MethodGet, "/", nil))
			results[i] = responses[0]
		}

		for i := range maxFailures {
			require.NotNil(t, results[i].err)
			assert.Equal(t, upstreamBadStatus, results[i].err.kind)
		}
		for i := maxFailures; i < 5; i++ {
			require.NotNil(t, results[i].err)
			assert.Equal(t, upstreamCircuitOpen, results[i].err.kind)
		}
		assert.Equal(t, int32(maxFailures), upstreamCalls.Load())
	})

	t.Run("closes after reset timeout and serves successful requests", func(t *testing.T) {
		resetTimeout := 100 * time.Millisecond
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

		f := newTestFlow([]upstream{
			newTestUpstream(server.URL, withCircuitBreaker(cb)),
		})

		d := newTestScatter()

		r1 := d.scatter(f, httptest.NewRequest(http.MethodGet, "/", nil))
		require.NotNil(t, r1[0].err)
		assert.Equal(t, upstreamBadStatus, r1[0].err.kind)

		r2 := d.scatter(f, httptest.NewRequest(http.MethodGet, "/", nil))
		require.NotNil(t, r2[0].err)
		assert.Equal(t, upstreamCircuitOpen, r2[0].err.kind)

		time.Sleep(resetTimeout + 20*time.Millisecond)

		r3 := d.scatter(f, httptest.NewRequest(http.MethodGet, "/", nil))
		assert.Nil(t, r3[0].err)
	})
}

func TestScatter_LoadBalancing(t *testing.T) {
	t.Run("distributes round-robin evenly across hosts", func(t *testing.T) {
		var callsA, callsB atomic.Int32

		serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callsA.Add(1)
		}))
		defer serverA.Close()

		serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callsB.Add(1)
		}))
		defer serverB.Close()

		f := newTestFlow([]upstream{
			newTestUpstream("",
				withHosts(serverA.URL, serverB.URL),
				withLBMode(lbModeRoundRobin, 2),
			),
		})

		d := newTestScatter()
		for range 4 {
			d.scatter(f, httptest.NewRequest(http.MethodGet, "/", nil))
		}

		assert.Equal(t, int32(2), callsA.Load())
		assert.Equal(t, int32(2), callsB.Load())
	})

	t.Run("least-connections sends more traffic to faster host", func(t *testing.T) {
		var callsA, callsB atomic.Int32

		serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callsA.Add(1)
			time.Sleep(100 * time.Millisecond)
		}))
		defer serverA.Close()

		serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callsB.Add(1)
		}))
		defer serverB.Close()

		f := newTestFlow([]upstream{
			newTestUpstream("",
				withHosts(serverA.URL, serverB.URL),
				withLBMode(lbModeLeastConns, 2),
			),
		})

		d := newTestScatter()
		var wg sync.WaitGroup
		for range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				d.scatter(f, httptest.NewRequest(http.MethodGet, "/", nil))
			}()
			time.Sleep(20 * time.Millisecond)
		}
		wg.Wait()

		assert.Greater(t, callsB.Load(), callsA.Load())
	})
}
