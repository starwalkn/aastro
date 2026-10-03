//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// gateway.server.http2: checked by the protocol the client ends up with.
func TestServerHTTP2(t *testing.T) {
	t.Parallel()

	t.Run("auto negotiates HTTP/2 over TLS", func(t *testing.T) {
		t.Parallel()

		resp, _ := get(t, env.mtlsClient, env.tlsBase+"/single/1", nil)
		assert.Equal(t, "HTTP/2.0", resp.Proto)
	})

	t.Run("off pins the data port to HTTP/1.1 even over TLS", func(t *testing.T) {
		t.Parallel()

		resp, _ := get(t, env.mtlsClient, env.tlsHTTP1Base+"/single/1", nil)
		assert.Equal(t, "HTTP/1.1", resp.Proto)
	})

	t.Run("auto without TLS serves no cleartext HTTP/2", func(t *testing.T) {
		t.Parallel()

		resp, _, err := tryGet(t, newH2CClient(), env.base+"/single/1", nil)
		if err == nil {
			assert.NotEqual(t, 2, resp.ProtoMajor, "h2c must not be served")
		}
	})
}

// upstreams[].transport.http2: checked by the protocol the upstream reports
// it was reached over.
func TestUpstreamHTTP2(t *testing.T) {
	t.Parallel()

	runCases(t, []tc{
		{
			name: "auto negotiates HTTP/2 with a TLS upstream",
			path: "/h2-upstream-auto",
			want: expect{status: http.StatusOK, json: j{"proto": "HTTP/2.0"}},
		},
		{
			name: "on uses HTTP/2 with a TLS upstream",
			path: "/h2-upstream-on",
			want: expect{status: http.StatusOK, json: j{"proto": "HTTP/2.0"}},
		},
		{
			name: "off pins the upstream to HTTP/1.1 even over TLS",
			path: "/h2-upstream-off",
			want: expect{status: http.StatusOK, json: j{"proto": "HTTP/1.1"}},
		},
	})
}

// newH2CClient speaks cleartext HTTP/2 with prior knowledge, never HTTP/1.1.
func newH2CClient() *http.Client {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)

	return &http.Client{
		Timeout:   clientTimeout,
		Transport: &http.Transport{Protocols: protocols},
	}
}
