package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"time"

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

var _ = Describe("Logger", func() {
	Describe("Handler", func() {
		It("logs the incoming request", func() {
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

			Expect(rec.Code).To(Equal(http.StatusOK))

			logOutput := buf.String()

			Expect(logOutput).To(ContainSubstring("request started"))
			Expect(logOutput).To(ContainSubstring("request completed"))
		})

		It("disabled and noes not log the incoming request", func() {
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

			Expect(rec.Code).To(Equal(http.StatusNoContent))
			Expect(buf.Len()).To(BeZero())
		})

		It("logs the incoming request with body", func() {
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

			Expect(rec.Code).To(Equal(http.StatusCreated))

			logOutput := buf.String()

			Expect(logOutput).To(ContainSubstring("request started"))
			Expect(logOutput).To(ContainSubstring("request completed"))
			Expect(logOutput).To(ContainSubstring("hello"))
			Expect(logOutput).To(ContainSubstring("world"))
		})
	})
})
