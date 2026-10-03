package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeHMACToken(secret []byte, issuer, audience string, exp time.Time) (string, error) {
	claims := jwt.MapClaims{
		"iss": issuer,
		"aud": audience,
		"exp": exp.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(secret)
	if err != nil {
		return "", err
	}

	return signed, nil
}

func newAuthTestMiddleware(secret []byte) *Middleware {
	return &Middleware{
		issuer:   "test-issuer",
		audience: "test-aud",
		realm:    defaultRealm,
		resolver: &hmacResolver{HMACSecret: secret},
		jwtConfig: jwtConfig{
			alg:        "HS256",
			hmacSecret: secret,
		},
	}
}

func TestAuthHandler(t *testing.T) {
	secret := []byte("secret")

	t.Run("no auth header", func(t *testing.T) {
		t.Run("returns unauthorized status code", func(t *testing.T) {
			m := newAuthTestMiddleware(secret)

			h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("ok"))
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Equal(t, `Bearer realm="restricted"`, rec.Header().Get("WWW-Authenticate"))
		})
	})

	t.Run("invalid bearer token", func(t *testing.T) {
		t.Run("returns unauthorized status code", func(t *testing.T) {
			m := newAuthTestMiddleware(secret)

			h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("ok"))
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer invalid-token")

			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Equal(t,
				`Bearer realm="restricted", error="invalid_token", error_description="invalid or expired token"`,
				rec.Header().Get("WWW-Authenticate"),
			)
		})
	})

	t.Run("expired bearer token", func(t *testing.T) {
		t.Run("returns unauthorized status code", func(t *testing.T) {
			m := newAuthTestMiddleware(secret)

			token, err := makeHMACToken(secret, "test-issuer", "test-aud", time.Now().Add(-time.Hour))
			require.NoError(t, err)

			h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("ok"))
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+token)

			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Equal(t,
				`Bearer realm="restricted", error="invalid_token", error_description="invalid or expired token"`,
				rec.Header().Get("WWW-Authenticate"),
			)
		})
	})

	t.Run("valid bearer token", func(t *testing.T) {
		t.Run("successfully passes the request", func(t *testing.T) {
			m := newAuthTestMiddleware(secret)

			token, err := makeHMACToken(secret, "test-issuer", "test-aud", time.Now().Add(time.Hour))
			require.NoError(t, err)

			var gotClaims *jwt.MapClaims

			h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				claims := r.Context().Value(ctxKeyClaims{}).(*jwt.MapClaims)
				gotClaims = claims

				w.Write([]byte("ok"))
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+token)

			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Empty(t, rec.Header().Get("WWW-Authenticate"))
			require.NotNil(t, gotClaims)

			issuer, err := gotClaims.GetIssuer()
			require.NoError(t, err)
			assert.Equal(t, "test-issuer", issuer)

			audience, err := gotClaims.GetAudience()
			require.NoError(t, err)
			assert.Contains(t, audience, "test-aud")
		})
	})

	t.Run("malformed authorization header", func(t *testing.T) {
		t.Run("returns invalid_request challenge", func(t *testing.T) {
			m := newAuthTestMiddleware(secret)

			h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("ok"))
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Basic abc123")

			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Equal(t,
				`Bearer realm="restricted", error="invalid_request", error_description="invalid authorization header"`,
				rec.Header().Get("WWW-Authenticate"),
			)
		})
	})
}

func TestBuildWWWAuthenticateHeader(t *testing.T) {
	t.Run("builds a realm-only Bearer challenge", func(t *testing.T) {
		assert.Equal(t, `Bearer realm="restricted"`, buildWWWAuthenticateHeader("restricted", "", ""))
	})

	t.Run("builds an invalid_token Bearer challenge", func(t *testing.T) {
		assert.Equal(t,
			`Bearer realm="custom-realm", error="invalid_token", error_description="invalid or expired token"`,
			buildWWWAuthenticateHeader("custom-realm", authErrorInvalidToken, "invalid or expired token"),
		)
	})

	t.Run("uses the default realm when realm is empty", func(t *testing.T) {
		assert.Equal(t,
			`Bearer realm="restricted", error="invalid_request", error_description="invalid authorization header"`,
			buildWWWAuthenticateHeader("", authErrorInvalidRequest, "invalid authorization header"),
		)
	})
}

func TestAuthInit(t *testing.T) {
	t.Run("uses a custom realm from config", func(t *testing.T) {
		middleware := &Middleware{}

		err := middleware.Init(map[string]interface{}{
			"issuer":      "test-issuer",
			"audience":    "test-aud",
			"alg":         "HS256",
			"hmac_secret": "c2VjcmV0",
			"realm":       "custom-realm",
		})

		require.NoError(t, err)
		assert.Equal(t, "custom-realm", middleware.realm)
	})

	t.Run("uses the default realm when none is configured", func(t *testing.T) {
		middleware := &Middleware{}

		err := middleware.Init(map[string]interface{}{
			"issuer":      "test-issuer",
			"audience":    "test-aud",
			"alg":         "HS256",
			"hmac_secret": "c2VjcmV0",
		})

		require.NoError(t, err)
		assert.Equal(t, defaultRealm, middleware.realm)
	})
}
