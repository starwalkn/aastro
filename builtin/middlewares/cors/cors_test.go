package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCORSMiddleware(cfg map[string]interface{}) *Middleware {
	m := &Middleware{}
	_ = m.Init(cfg)
	return m
}

func passthroughHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func newCORSTest() (*httptest.ResponseRecorder, *http.Request) {
	return httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)
}

// newPreflightTest mirrors the "preflight" Context's nested BeforeEach.
func newPreflightTest() (*httptest.ResponseRecorder, *http.Request) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://myapp.com")
	req.Header.Set("Access-Control-Request-Method", "POST")

	return rec, req
}

func TestCORSInit(t *testing.T) {
	t.Run("rejects credentials combined with wildcard origin", func(t *testing.T) {
		m := &Middleware{}
		err := m.Init(map[string]interface{}{
			"allowed_origins":   []interface{}{"*"},
			"allow_credentials": true,
		})

		assert.ErrorContains(t, err, "cannot be used with wildcard origin")
	})

	t.Run("succeeds with explicit origins and credentials", func(t *testing.T) {
		m := &Middleware{}
		err := m.Init(map[string]interface{}{
			"allowed_origins":   []interface{}{"https://myapp.com"},
			"allow_credentials": true,
		})

		require.NoError(t, err)
	})
}

func TestCORSHandler(t *testing.T) {
	t.Run("without an Origin header", func(t *testing.T) {
		t.Run("passes the request through without CORS headers", func(t *testing.T) {
			rec, req := newCORSTest()

			m := newCORSMiddleware(map[string]interface{}{
				"allowed_origins": []interface{}{"https://myapp.com"},
			})

			m.Handler(passthroughHandler()).ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
			assert.Empty(t, rec.Header().Get("Vary"))
		})
	})

	t.Run("origin handling", func(t *testing.T) {
		tests := []struct {
			name                string
			allowedOrigins      []interface{}
			requestOrigin       string
			expectedStatus      int
			expectedAllowOrigin string
		}{
			{
				name:                "allowed explicit origin echoes back",
				allowedOrigins:      []interface{}{"https://myapp.com"},
				requestOrigin:       "https://myapp.com",
				expectedStatus:      http.StatusOK,
				expectedAllowOrigin: "https://myapp.com",
			},
			{
				name:                "disallowed origin returns 403",
				allowedOrigins:      []interface{}{"https://myapp.com"},
				requestOrigin:       "https://evil.com",
				expectedStatus:      http.StatusForbidden,
				expectedAllowOrigin: "",
			},
			{
				name:                "wildcard responds with star",
				allowedOrigins:      []interface{}{"*"},
				requestOrigin:       "https://anyone.com",
				expectedStatus:      http.StatusOK,
				expectedAllowOrigin: "*",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rec, req := newCORSTest()

				m := newCORSMiddleware(map[string]interface{}{
					"allowed_origins": tt.allowedOrigins,
				})

				req.Header.Set("Origin", tt.requestOrigin)
				m.Handler(passthroughHandler()).ServeHTTP(rec, req)

				assert.Equal(t, tt.expectedStatus, rec.Code)
				assert.Equal(t, tt.expectedAllowOrigin, rec.Header().Get("Access-Control-Allow-Origin"))
			})
		}
	})

	t.Run("Vary header", func(t *testing.T) {
		t.Run("is set to Origin for explicit allowed origins", func(t *testing.T) {
			rec, req := newCORSTest()

			m := newCORSMiddleware(map[string]interface{}{
				"allowed_origins": []interface{}{"https://myapp.com"},
			})

			req.Header.Set("Origin", "https://myapp.com")
			m.Handler(passthroughHandler()).ServeHTTP(rec, req)

			assert.Equal(t, "Origin", rec.Header().Get("Vary"))
		})

		t.Run("is not set for wildcard origin", func(t *testing.T) {
			rec, req := newCORSTest()

			m := newCORSMiddleware(map[string]interface{}{
				"allowed_origins": []interface{}{"*"},
			})

			req.Header.Set("Origin", "https://anyone.com")
			m.Handler(passthroughHandler()).ServeHTTP(rec, req)

			assert.Empty(t, rec.Header().Get("Vary"))
		})
	})

	t.Run("with allow_credentials", func(t *testing.T) {
		t.Run("emits Access-Control-Allow-Credentials: true", func(t *testing.T) {
			rec, req := newCORSTest()

			m := newCORSMiddleware(map[string]interface{}{
				"allowed_origins":   []interface{}{"https://myapp.com"},
				"allow_credentials": true,
			})

			req.Header.Set("Origin", "https://myapp.com")
			m.Handler(passthroughHandler()).ServeHTTP(rec, req)

			assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
		})
	})

	t.Run("preflight", func(t *testing.T) {
		t.Run("responds with 204 and includes allowed methods and headers", func(t *testing.T) {
			rec, req := newPreflightTest()

			m := newCORSMiddleware(map[string]interface{}{
				"allowed_origins": []interface{}{"https://myapp.com"},
				"allowed_methods": []interface{}{"GET", "POST"},
				"allowed_headers": []interface{}{"Content-Type", "Authorization"},
			})

			m.Handler(passthroughHandler()).ServeHTTP(rec, req)

			assert.Equal(t, http.StatusNoContent, rec.Code)
			assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Methods"))
			assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Headers"))
		})

		t.Run("does not invoke the wrapped handler", func(t *testing.T) {
			rec, req := newPreflightTest()

			m := newCORSMiddleware(map[string]interface{}{
				"allowed_origins": []interface{}{"https://myapp.com"},
				"allowed_methods": []interface{}{"GET", "POST"},
			})

			var reached bool
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
			})

			m.Handler(handler).ServeHTTP(rec, req)

			assert.False(t, reached)
		})
	})

	t.Run("OPTIONS without preflight headers", func(t *testing.T) {
		t.Run("passes through to the wrapped handler", func(t *testing.T) {
			rec, _ := newCORSTest()

			m := newCORSMiddleware(map[string]interface{}{
				"allowed_origins": []interface{}{"https://myapp.com"},
			})

			req := httptest.NewRequest(http.MethodOptions, "/", nil)
			req.Header.Set("Origin", "https://myapp.com")

			var reached bool
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			})

			m.Handler(handler).ServeHTTP(rec, req)

			assert.True(t, reached)
		})
	})
}
