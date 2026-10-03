//go:build integration

package integration

import (
	"net/http"
	"net/url"
	"testing"
)

func TestSingleUpstream(t *testing.T) {
	t.Parallel()

	htmlNotFound := "/single/1?status=404&content_type=text/html&body=" +
		url.QueryEscape("<html><body>not found</body></html>")

	runCases(t, []tc{
		{
			name: "success is proxied as-is, without an envelope",
			path: "/single/1",
			want: expect{status: http.StatusOK, json: j{"name": "Alice Novak"}, noJSON: []string{"data"}},
		},
		{
			name: "404 propagated verbatim",
			path: "/single/1?status=404",
			want: expect{status: http.StatusNotFound},
		},
		{
			name: "401 propagated verbatim",
			path: "/single/1?status=401",
			want: expect{status: http.StatusUnauthorized},
		},
		{
			name: "403 propagated verbatim",
			path: "/single/1?status=403",
			want: expect{status: http.StatusForbidden},
		},
		{
			name: "4xx with HTML body passes through untouched",
			path: htmlNotFound,
			want: expect{status: http.StatusNotFound, header: h{"Content-Type": "text/html"}, bodyContains: "<html>"},
		},
		{
			name: "5xx becomes 502 without leaking the upstream body",
			path: "/single/1?status=500",
			want: expect{status: http.StatusBadGateway, json: j{"errors.0": "UPSTREAM_ERROR"}},
		},
		{
			name: "204 keeps its status",
			path: "/single/1?status=204",
			want: expect{status: http.StatusNoContent},
		},
		{
			name: "redirect is not followed by default",
			path: "/single/1?redirect=/single/2",
			want: expect{status: http.StatusFound, header: h{"Location": "/single/2"}},
		},
		{
			name: "redirect is followed with follow_redirects",
			path: "/single-follow/1?redirect=/users/2",
			want: expect{status: http.StatusOK, json: j{"name": "Bob Tanaka"}},
		},
		{
			name: "dropped connection becomes 502",
			path: "/single/2?drop=1",
			want: expect{status: http.StatusBadGateway},
		},
	})
}
