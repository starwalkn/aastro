//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Runs against the dedicated gateway with client_auth: require.
func TestMTLS(t *testing.T) {
	t.Parallel()

	t.Run("valid client certificate is accepted and proxied", func(t *testing.T) {
		t.Parallel()

		resp, body := get(t, env.mtlsClient, env.tlsBase+"/single/1", nil)
		expect{status: http.StatusOK, json: j{"name": "Alice Novak"}}.check(t, resp, body)
	})

	t.Run("no client certificate fails the handshake", func(t *testing.T) {
		t.Parallel()

		_, _, err := tryGet(t, env.noCertTLS, env.tlsBase+"/single/1", nil)
		require.Error(t, err)
	})

	t.Run("client certificate from an untrusted CA fails the handshake", func(t *testing.T) {
		t.Parallel()

		_, _, err := tryGet(t, env.rogueTLS, env.tlsBase+"/single/1", nil)
		require.Error(t, err)
	})

	t.Run("plaintext request to the TLS port is rejected", func(t *testing.T) {
		t.Parallel()

		resp, _ := get(t, env.client, env.tlsPlainURL+"/single/1", nil)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("streaming works end-to-end over mTLS", func(t *testing.T) {
		t.Parallel()

		_, body := get(t, env.mtlsClient, env.tlsBase+"/users/1/events?events=2", nil)
		assert.Equal(t, 2, countEvents(body))
	})
}
