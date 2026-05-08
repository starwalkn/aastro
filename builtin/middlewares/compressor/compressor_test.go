package main

import (
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Compressor", func() {
	Describe("Handler", func() {
		Context("gzip", func() {
			It("compress data to gzip", func() {
				m := &Middleware{
					enabled: true,
					alg:     algGzip,
				}

				h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Write([]byte("hello gzip"))
				}))

				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Accept-Encoding", "gzip")

				rec := httptest.NewRecorder()

				h.ServeHTTP(rec, req)

				Expect(rec.Header().Get("Content-Encoding")).To(Equal("gzip"))

				r, err := gzip.NewReader(rec.Body)
				Expect(err).ToNot(HaveOccurred())

				defer r.Close()

				data, err := io.ReadAll(r)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(data)).To(Equal("hello gzip"))
			})
		})

		Context("deflate", func() {
			It("compress data to deflate", func() {
				m := &Middleware{
					enabled: true,
					alg:     algDeflate,
				}

				h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Write([]byte("hello deflate"))
				}))

				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Accept-Encoding", "deflate")

				rec := httptest.NewRecorder()

				h.ServeHTTP(rec, req)

				Expect(rec.Header().Get("Content-Encoding")).To(Equal("deflate"))

				r := flate.NewReader(rec.Body)
				defer r.Close()

				data, err := io.ReadAll(r)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(data)).To(Equal("hello deflate"))
			})
		})

		Context("without encoding header", func() {
			It("returns plain data without compression", func() {
				m := &Middleware{
					enabled: true,
					alg:     algGzip,
				}

				h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Write([]byte("hello gzip"))
				}))

				req := httptest.NewRequest(http.MethodGet, "/", nil)
				rec := httptest.NewRecorder()

				h.ServeHTTP(rec, req)

				Expect(rec.Header().Get("Content-Encoding")).To(Equal(""))
				Expect(rec.Body.String()).To(Equal("hello gzip"))
			})
		})
	})
})
