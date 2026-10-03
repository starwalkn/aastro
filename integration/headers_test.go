//go:build integration

package integration

import (
	"net/http"
	"testing"
)

func TestHeaders(t *testing.T) {
	t.Parallel()

	runCases(t, []tc{
		{
			name:      "forward_headers passes listed and prefix-matched headers, blocks the rest",
			path:      "/single-headers/1?echo_headers=1",
			reqHeader: h{"X-Allowed": "yes", "X-Prefix-Foo": "yes", "X-Not-Listed": "leak-check"},
			want: expect{
				json:   j{"headers.X-Allowed.0": "yes", "headers.X-Prefix-Foo.0": "yes"},
				noJSON: []string{"headers.X-Not-Listed"},
			},
		},
		{
			name: "header_blacklist strips a listed header",
			path: "/single-headers/1?resp_header=X-Secret:leaked",
			want: expect{status: http.StatusOK, header: h{"X-Secret": absent}},
		},
		{
			name: "header_blacklist leaves unlisted headers alone",
			path: "/single-headers/1?resp_header=X-Keep:visible",
			want: expect{header: h{"X-Keep": "visible"}},
		},
		{
			name: "header_blacklist matches case-insensitively",
			path: "/single-headers/1?resp_header=X-Lower-Secret:leaked",
			want: expect{header: h{"X-Lower-Secret": absent}},
		},
		{
			name: "hop-by-hop TE is stripped, ordinary header kept",
			path: "/single/1?resp_header=TE:trailers&resp_header=X-Custom:kept",
			want: expect{header: h{"TE": absent, "X-Custom": "kept"}},
		},
		{
			name: "hop-by-hop Proxy-Authenticate is stripped",
			path: "/single/1?resp_header=Proxy-Authenticate:Basic",
			want: expect{header: h{"Proxy-Authenticate": absent}},
		},
		{
			name: "hop-by-hop Connection is stripped",
			path: "/single/1?resp_header=Connection:close",
			want: expect{header: h{"Connection": absent}},
		},
	})
}
