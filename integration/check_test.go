//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// absent, as an expected header value, means the header must not be present.
const absent = "\x00absent"

type (
	h = map[string]string
	j = map[string]string
)

// tc is one table-driven case: a single GET against the plain gateway, and
// everything expected of that one response.
type tc struct {
	name      string
	path      string
	reqHeader h
	want      expect
}

type expect struct {
	status       int
	header       h
	json         j
	noJSON       []string
	bodyContains string
}

func runCases(t *testing.T, cases []tc) {
	t.Helper()

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			resp, body := get(t, env.client, env.base+c.path, c.reqHeader)
			c.want.check(t, resp, body)
		})
	}
}

func (e expect) check(t *testing.T, resp *http.Response, body []byte) {
	t.Helper()

	if e.status != 0 {
		assert.Equal(t, e.status, resp.StatusCode, "status; body: %s", body)
	}

	for name, want := range e.header {
		if want == absent {
			assert.NotContains(t, resp.Header, http.CanonicalHeaderKey(name), "header %s must be absent", name)
			continue
		}

		assert.Equal(t, want, resp.Header.Get(name), "header %s", name)
	}

	if e.bodyContains != "" {
		assert.Contains(t, string(body), e.bodyContains)
	}

	if len(e.json) == 0 && len(e.noJSON) == 0 {
		return
	}

	var doc any
	require.NoError(t, json.Unmarshal(body, &doc), "body is not JSON: %s", body)

	for path, want := range e.json {
		got, ok := lookup(doc, path)
		if assert.True(t, ok, "json path %q missing; body: %s", path, body) {
			assert.Equal(t, want, fmt.Sprint(got), "json path %q", path)
		}
	}

	for _, path := range e.noJSON {
		_, ok := lookup(doc, path)
		assert.False(t, ok, "json path %q must be absent; body: %s", path, body)
	}
}

func lookup(doc any, path string) (any, bool) {
	cur := doc

	for seg := range strings.SplitSeq(path, ".") {
		switch v := cur.(type) {
		case map[string]any:
			if seg == "#" {
				return len(v), true
			}

			next, ok := v[seg]
			if !ok {
				return nil, false
			}

			cur = next
		case []any:
			if seg == "#" {
				return len(v), true
			}

			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(v) {
				return nil, false
			}

			cur = v[i]
		default:
			return nil, false
		}
	}

	return cur, true
}

// get performs a GET and returns the response with its fully read body.
func get(t *testing.T, client *http.Client, url string, reqHeader h) (*http.Response, []byte) {
	t.Helper()

	resp, body, err := tryGet(t, client, url, reqHeader)
	require.NoError(t, err)

	return resp, body
}

// tryGet is get for cases where the request itself is expected to fail.
func tryGet(t *testing.T, client *http.Client, url string, reqHeader h) (*http.Response, []byte, error) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)

	for k, v := range reqHeader {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)

	return resp, body, err
}

func expectStatus(t *testing.T, path string, status int) {
	t.Helper()

	resp, body := get(t, env.client, env.base+path, nil)
	assert.Equal(t, status, resp.StatusCode, "GET %s; body: %s", path, body)
}

// countEvents counts SSE "data:" lines in a stream body.
func countEvents(body []byte) int {
	n := 0

	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "data:") {
			n++
		}
	}

	return n
}
