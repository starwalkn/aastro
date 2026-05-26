package main

import (
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("Auth", func() {
	Describe("Handler", func() {
		var (
			secret []byte
			m      *Middleware
		)

		BeforeEach(func() {
			secret = []byte("secret")
			m = &Middleware{
				issuer:   "test-issuer",
				audience: "test-aud",
				realm:    defaultRealm,
				resolver: &hmacResolver{HMACSecret: secret},
				jwtConfig: jwtConfig{
					alg:        "HS256",
					hmacSecret: secret,
				},
			}
		})

		Context("no auth header", func() {
			It("returns unauthorized status code", func() {
				h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte("ok"))
				}))

				req := httptest.NewRequest(http.MethodGet, "/", nil)
				rec := httptest.NewRecorder()

				h.ServeHTTP(rec, req)

				Expect(rec.Code).To(Equal(http.StatusUnauthorized))
				Expect(rec.Header().Get("WWW-Authenticate")).To(Equal(`Bearer realm="aastro"`))
			})
		})

		Context("invalid bearer token", func() {
			It("returns unauthorized status code", func() {
				h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte("ok"))
				}))

				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Authorization", "Bearer invalid-token")

				rec := httptest.NewRecorder()

				h.ServeHTTP(rec, req)

				Expect(rec.Code).To(Equal(http.StatusUnauthorized))
				Expect(rec.Header().Get("WWW-Authenticate")).To(Equal(
					`Bearer realm="aastro", error="invalid_token", error_description="invalid or expired token"`,
				))
			})
		})

		Context("expired bearer token", func() {
			It("returns unauthorized status code", func() {
				token, err := makeHMACToken(secret, "test-issuer", "test-aud", time.Now().Add(-time.Hour))
				Expect(err).ToNot(HaveOccurred())

				h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte("ok"))
				}))

				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Authorization", "Bearer "+token)

				rec := httptest.NewRecorder()

				h.ServeHTTP(rec, req)

				Expect(rec.Code).To(Equal(http.StatusUnauthorized))
				Expect(rec.Header().Get("WWW-Authenticate")).To(Equal(
					`Bearer realm="aastro", error="invalid_token", error_description="invalid or expired token"`,
				))
			})
		})

		Context("valid bearer token", func() {
			It("successfully passes the request", func() {
				token, err := makeHMACToken(secret, "test-issuer", "test-aud", time.Now().Add(time.Hour))
				Expect(err).ToNot(HaveOccurred())

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

				Expect(rec.Code).To(Equal(http.StatusOK))
				Expect(rec.Header().Get("WWW-Authenticate")).To(BeEmpty())
				Expect(gotClaims).ToNot(BeNil())
				Expect(gotClaims.GetIssuer()).To(Equal("test-issuer"))
				Expect(gotClaims.GetAudience()).To(ContainElement("test-aud"))

			})
		})
		Context("malformed authorization header", func() {
			It("returns invalid_request challenge", func() {
				h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("ok"))
				}))

				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Authorization", "Basic abc123")

				rec := httptest.NewRecorder()

				h.ServeHTTP(rec, req)

				Expect(rec.Code).To(Equal(http.StatusUnauthorized))
				Expect(rec.Header().Get("WWW-Authenticate")).To(Equal(
					`Bearer realm="aastro", error="invalid_request", error_description="invalid authorization header"`,
				))
			})
		})
		Describe("buildWWWAuthenticateHeader", func() {
			It("builds a realm-only Bearer challenge", func() {
				Expect(buildWWWAuthenticateHeader("aastro", "", "")).To(Equal(`Bearer realm="aastro"`))
			})

			It("builds an invalid_token Bearer challenge", func() {
				Expect(buildWWWAuthenticateHeader("aastro", authErrorInvalidToken, "invalid or expired token")).To(Equal(
					`Bearer realm="aastro", error="invalid_token", error_description="invalid or expired token"`,
				))
			})

			It("uses the default realm when realm is empty", func() {
				Expect(buildWWWAuthenticateHeader("", authErrorInvalidRequest, "invalid authorization header")).To(Equal(
					`Bearer realm="aastro", error="invalid_request", error_description="invalid authorization header"`,
				))
			})
		})
		Describe("Init", func() {
			It("uses a custom realm from config", func() {
				middleware := &Middleware{}

				err := middleware.Init(map[string]interface{}{
					"issuer":      "test-issuer",
					"audience":    "test-aud",
					"alg":         "HS256",
					"hmac_secret": "c2VjcmV0",
					"realm":       "custom-realm",
				})

				Expect(err).NotTo(HaveOccurred())
				Expect(middleware.realm).To(Equal("custom-realm"))
			})

			It("uses the default realm when none is configured", func() {
				middleware := &Middleware{}

				err := middleware.Init(map[string]interface{}{
					"issuer":      "test-issuer",
					"audience":    "test-aud",
					"alg":         "HS256",
					"hmac_secret": "c2VjcmV0",
				})

				Expect(err).NotTo(HaveOccurred())
				Expect(middleware.realm).To(Equal(defaultRealm))
			})
		})
	})
})
