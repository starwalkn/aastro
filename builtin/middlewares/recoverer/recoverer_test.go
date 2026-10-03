package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/voidrunner3074/aastro"
)

func newTestLogger(buf *bytes.Buffer) *zap.Logger {
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(buf),
		zapcore.DebugLevel,
	)

	return zap.New(core)
}

func TestRecovererHandle(t *testing.T) {
	t.Run("recovers the panic", func(t *testing.T) {
		buf := new(bytes.Buffer)
		m := &Middleware{
			enabled: true,
			log:     newTestLogger(buf),
		}

		h := m.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("boom")
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))

		var problem aastro.ProblemDetails
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
		assert.Equal(t, http.StatusInternalServerError, problem.Status)
		assert.Equal(t, "Internal gateway error", problem.Title)

		logOutput := buf.String()
		assert.Contains(t, logOutput, "panic recovered")
	})

	t.Run("recovers the panic and include stacktrace", func(t *testing.T) {
		buf := new(bytes.Buffer)
		m := &Middleware{
			enabled:      true,
			log:          newTestLogger(buf),
			includeStack: true,
		}

		h := m.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("boom")
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))

		var problem aastro.ProblemDetails
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
		assert.Equal(t, http.StatusInternalServerError, problem.Status)
		assert.Equal(t, "Internal gateway error", problem.Title)

		logOutput := buf.String()
		assert.Contains(t, logOutput, "panic recovered")
		assert.Contains(t, logOutput, "stack")
	})
}
