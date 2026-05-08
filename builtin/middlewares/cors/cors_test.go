package main

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("CORS", func() {
	var (
		rec *httptest.ResponseRecorder
		req *http.Request
	)

	BeforeEach(func() {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/", nil)
	})

	Describe("Init", func() {
		It("rejects credentials combined with wildcard origin", func() {
			m := &Middleware{}
			err := m.Init(map[string]interface{}{
				"allowed_origins":   []interface{}{"*"},
				"allow_credentials": true,
			})

			Expect(err).To(MatchError(ContainSubstring("cannot be used with wildcard origin")))
		})

		It("succeeds with explicit origins and credentials", func() {
			m := &Middleware{}
			err := m.Init(map[string]interface{}{
				"allowed_origins":   []interface{}{"https://myapp.com"},
				"allow_credentials": true,
			})

			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Handler", func() {
		Context("without an Origin header", func() {
			It("passes the request through without CORS headers", func() {
				m := newCORSMiddleware(map[string]interface{}{
					"allowed_origins": []interface{}{"https://myapp.com"},
				})

				m.Handler(passthroughHandler()).ServeHTTP(rec, req)

				Expect(rec.Code).To(Equal(http.StatusOK))
				Expect(rec.Header().Get("Access-Control-Allow-Origin")).To(BeEmpty())
				Expect(rec.Header().Get("Vary")).To(BeEmpty())
			})
		})

		DescribeTable("origin handling",
			func(allowedOrigins []interface{}, requestOrigin string, expectedStatus int, expectedAllowOrigin string) {
				m := newCORSMiddleware(map[string]interface{}{
					"allowed_origins": allowedOrigins,
				})

				req.Header.Set("Origin", requestOrigin)
				m.Handler(passthroughHandler()).ServeHTTP(rec, req)

				Expect(rec.Code).To(Equal(expectedStatus))
				Expect(rec.Header().Get("Access-Control-Allow-Origin")).To(Equal(expectedAllowOrigin))
			},
			Entry("allowed explicit origin echoes back",
				[]interface{}{"https://myapp.com"}, "https://myapp.com",
				http.StatusOK, "https://myapp.com"),
			Entry("disallowed origin returns 403",
				[]interface{}{"https://myapp.com"}, "https://evil.com",
				http.StatusForbidden, ""),
			Entry("wildcard responds with star",
				[]interface{}{"*"}, "https://anyone.com",
				http.StatusOK, "*"),
		)

		Context("Vary header", func() {
			It("is set to Origin for explicit allowed origins", func() {
				m := newCORSMiddleware(map[string]interface{}{
					"allowed_origins": []interface{}{"https://myapp.com"},
				})

				req.Header.Set("Origin", "https://myapp.com")
				m.Handler(passthroughHandler()).ServeHTTP(rec, req)

				Expect(rec.Header().Get("Vary")).To(Equal("Origin"))
			})

			It("is not set for wildcard origin", func() {
				m := newCORSMiddleware(map[string]interface{}{
					"allowed_origins": []interface{}{"*"},
				})

				req.Header.Set("Origin", "https://anyone.com")
				m.Handler(passthroughHandler()).ServeHTTP(rec, req)

				Expect(rec.Header().Get("Vary")).To(BeEmpty())
			})
		})

		Context("with allow_credentials", func() {
			It("emits Access-Control-Allow-Credentials: true", func() {
				m := newCORSMiddleware(map[string]interface{}{
					"allowed_origins":   []interface{}{"https://myapp.com"},
					"allow_credentials": true,
				})

				req.Header.Set("Origin", "https://myapp.com")
				m.Handler(passthroughHandler()).ServeHTTP(rec, req)

				Expect(rec.Header().Get("Access-Control-Allow-Credentials")).To(Equal("true"))
			})
		})

		Context("preflight", func() {
			BeforeEach(func() {
				req = httptest.NewRequest(http.MethodOptions, "/", nil)
				req.Header.Set("Origin", "https://myapp.com")
				req.Header.Set("Access-Control-Request-Method", "POST")
			})

			It("responds with 204 and includes allowed methods and headers", func() {
				m := newCORSMiddleware(map[string]interface{}{
					"allowed_origins": []interface{}{"https://myapp.com"},
					"allowed_methods": []interface{}{"GET", "POST"},
					"allowed_headers": []interface{}{"Content-Type", "Authorization"},
				})

				m.Handler(passthroughHandler()).ServeHTTP(rec, req)

				Expect(rec.Code).To(Equal(http.StatusNoContent))
				Expect(rec.Header().Get("Access-Control-Allow-Methods")).NotTo(BeEmpty())
				Expect(rec.Header().Get("Access-Control-Allow-Headers")).NotTo(BeEmpty())
			})

			It("does not invoke the wrapped handler", func() {
				m := newCORSMiddleware(map[string]interface{}{
					"allowed_origins": []interface{}{"https://myapp.com"},
					"allowed_methods": []interface{}{"GET", "POST"},
				})

				var reached bool
				handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					reached = true
				})

				m.Handler(handler).ServeHTTP(rec, req)

				Expect(reached).To(BeFalse())
			})
		})

		Context("OPTIONS without preflight headers", func() {
			It("passes through to the wrapped handler", func() {
				m := newCORSMiddleware(map[string]interface{}{
					"allowed_origins": []interface{}{"https://myapp.com"},
				})

				req = httptest.NewRequest(http.MethodOptions, "/", nil)
				req.Header.Set("Origin", "https://myapp.com")

				var reached bool
				handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					reached = true
					w.WriteHeader(http.StatusOK)
				})

				m.Handler(handler).ServeHTTP(rec, req)

				Expect(reached).To(BeTrue())
			})
		})
	})
})
