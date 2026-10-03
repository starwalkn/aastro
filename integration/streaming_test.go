//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreaming(t *testing.T) {
	t.Parallel()

	runCases(t, []tc{
		{
			name: "SSE passes through", path: "/users/1/events?events=1",
			want: expect{
				status:       http.StatusOK,
				header:       h{"Content-Type": "text/event-stream"},
				bodyContains: "data:",
			},
		},
		{
			name: "upstream failing before the first write reaches the client", path: "/users/1/events?status=404",
			want: expect{status: http.StatusNotFound},
		},
		// The streaming path copies headers through its own loop, so check it
		// strips hop-by-hop headers too rather than assuming it does.
		{
			name: "hop-by-hop stripped on the streaming path, ordinary header kept",
			path: "/users/1/events?events=1&resp_header=TE:trailers&resp_header=X-Custom:kept",
			want: expect{header: h{"TE": absent, "X-Custom": "kept"}},
		},
	})

	t.Run("exact event count is respected", func(t *testing.T) {
		t.Parallel()

		_, body := get(t, env.client, env.base+"/users/1/events?events=3", nil)
		assert.Equal(t, 3, countEvents(body))
	})
}
