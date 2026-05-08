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
				Expect(gotClaims).ToNot(BeNil())
				Expect(gotClaims.GetIssuer()).To(Equal("test-issuer"))
				Expect(gotClaims.GetAudience()).To(ContainElement("test-aud"))
			})
		})
	})
})
