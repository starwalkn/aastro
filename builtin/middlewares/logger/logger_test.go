package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func newTestLogger(buf *bytes.Buffer) *zap.Logger {
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(buf),
		zapcore.DebugLevel,
	)

	return zap.New(core)
}

func TestLoggerHandler(t *testing.T) {
	t.Run("logs the incoming request", func(t *testing.T) {
		buf := new(bytes.Buffer)
		m := &Middleware{
			log:     newTestLogger(buf),
			enabled: true,
		}

		h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(10 * time.Millisecond)

			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		logOutput := buf.String()

		assert.Contains(t, logOutput, "request started")
		assert.Contains(t, logOutput, "request completed")
	})

	t.Run("disabled and noes not log the incoming request", func(t *testing.T) {
		buf := new(bytes.Buffer)
		m := &Middleware{
			log:     newTestLogger(buf),
			enabled: false,
		}

		h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.Zero(t, buf.Len())
	})

	t.Run("logs the incoming request with body", func(t *testing.T) {
		buf := new(bytes.Buffer)
		m := &Middleware{
			log:     newTestLogger(buf),
			enabled: true,
			logBody: true,
		}

		h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", bytes.NewBufferString(`{"hello":"world"}`))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)

		logOutput := buf.String()

		assert.Contains(t, logOutput, "request started")
		assert.Contains(t, logOutput, "request completed")
		assert.Contains(t, logOutput, "hello")
		assert.Contains(t, logOutput, "world")
	})
}
