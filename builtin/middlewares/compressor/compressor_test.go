package main

import (
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompressorHandler(t *testing.T) {
	t.Run("gzip", func(t *testing.T) {
		t.Run("compress data to gzip", func(t *testing.T) {
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

			assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))

			r, err := gzip.NewReader(rec.Body)
			require.NoError(t, err)

			defer r.Close()

			data, err := io.ReadAll(r)
			require.NoError(t, err)
			assert.Equal(t, "hello gzip", string(data))
		})
	})

	t.Run("deflate", func(t *testing.T) {
		t.Run("compress data to deflate", func(t *testing.T) {
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

			assert.Equal(t, "deflate", rec.Header().Get("Content-Encoding"))

			r := flate.NewReader(rec.Body)
			defer r.Close()

			data, err := io.ReadAll(r)
			require.NoError(t, err)
			assert.Equal(t, "hello deflate", string(data))
		})
	})

	t.Run("without encoding header", func(t *testing.T) {
		t.Run("returns plain data without compression", func(t *testing.T) {
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

			assert.Empty(t, rec.Header().Get("Content-Encoding"))
			assert.Equal(t, "hello gzip", rec.Body.String())
		})
	})
}
