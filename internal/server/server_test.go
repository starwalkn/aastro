package server

import "testing"

func TestBuildProtocols(t *testing.T) {
	t.Run("auto leaves Go's own TLS-dependent default in place", func(t *testing.T) {
		if p := buildProtocols("auto"); p != nil {
			t.Fatalf("expected nil Protocols for auto, got %+v", p)
		}
	})

	t.Run("empty string behaves like auto", func(t *testing.T) {
		if p := buildProtocols(""); p != nil {
			t.Fatalf("expected nil Protocols for empty string, got %+v", p)
		}
	})

	t.Run("on enables HTTP/1 and HTTP/2", func(t *testing.T) {
		p := buildProtocols("on")
		if p == nil {
			t.Fatal("expected non-nil Protocols for on")
		}

		if !p.HTTP1() || !p.HTTP2() {
			t.Fatalf("expected HTTP1 and HTTP2 both enabled, got %+v", p)
		}
	})

	t.Run("off pins the server to HTTP/1 only", func(t *testing.T) {
		p := buildProtocols("off")
		if p == nil {
			t.Fatal("expected non-nil Protocols for off")
		}

		if !p.HTTP1() || p.HTTP2() {
			t.Fatalf("expected HTTP1 only, got %+v", p)
		}
	})
}
