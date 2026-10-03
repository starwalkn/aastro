//go:build integration

package integration

import (
	"net/http"
	"testing"
)

func TestMerge(t *testing.T) {
	t.Parallel()

	runCases(t, []tc{
		{
			name: "all upstreams answer: fields from each are merged",
			path: "/merge/1",
			want: expect{status: http.StatusOK, json: j{"name": "Alice Novak", "posts": "142", "theme": "dark"}},
		},
		{
			name: "one upstream fails: 206 with the others' fields", path: "/merge/1?stats_status=500",
			want: expect{
				status: http.StatusPartialContent,
				header: h{"X-Partial-Errors": "UPSTREAM_ERROR"},
				json:   j{"name": "Alice Novak", "theme": "dark"},
				noJSON: []string{"posts"},
			},
		},
	})
}

func TestArray(t *testing.T) {
	t.Parallel()

	runCases(t, []tc{
		{
			name: "both upstreams answer: bodies in upstream order",
			path: "/array/1",
			want: expect{status: http.StatusOK, json: j{"#": "2", "0.name": "Alice Novak", "1.posts": "142"}},
		},
		{
			name: "upstreams agree on 401: client sees 401",
			path: "/array/1?status=401",
			want: expect{status: http.StatusUnauthorized},
		},
		{
			name: "upstreams disagree (401 vs 500): 502",
			path: "/array/1?profile_status=401&stats_status=500",
			want: expect{status: http.StatusBadGateway},
		},
		{
			name: "one upstream fails without best_effort: 502, not partial",
			path: "/array/1?stats_status=500",
			want: expect{status: http.StatusBadGateway},
		},
	})
}
