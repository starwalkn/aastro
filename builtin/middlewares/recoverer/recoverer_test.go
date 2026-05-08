package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("Recoverer", func() {
	Describe("Handle", func() {
		It("recovers the panic", func() {
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

			Expect(rec.Code).To(Equal(http.StatusInternalServerError))

			body := rec.Body.String()
			Expect(body).To(Equal(`{"errors":[{"code":"INTERNAL"}]}`))

			logOutput := buf.String()
			Expect(logOutput).To(ContainSubstring("panic recovered"))
		})

		It("recovers the panic and include stacktrace", func() {
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

			Expect(rec.Code).To(Equal(http.StatusInternalServerError))

			body := rec.Body.String()
			Expect(body).To(Equal(`{"errors":[{"code":"INTERNAL"}]}`))

			logOutput := buf.String()
			Expect(logOutput).To(ContainSubstring("panic recovered"))
			Expect(logOutput).To(ContainSubstring("stack"))
		})
	})
})
