//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"
)

type obj = map[string]any

var (
	profiles = map[string]obj{
		"1": {"id": "1", "name": "Alice Novak", "email": "alice@example.com", "bio": "Engineer at Acme Corp"},
		"2": {"id": "2", "name": "Bob Tanaka", "email": "bob@example.com", "bio": "Designer and coffee lover"},
		"3": {"id": "3", "name": "Carol Deschamps", "email": "carol@example.com", "bio": "Product manager, cat person"},
	}

	stats = map[string]obj{
		"1": {"id": "1", "posts": 142, "followers": 890, "following": 230, "last_active": "2026-04-14T18:22:00Z"},
		"2": {"id": "2", "posts": 37, "followers": 412, "following": 189, "last_active": "2026-04-15T09:05:00Z"},
		"3": {"id": "3", "posts": 289, "followers": 1540, "following": 76, "last_active": "2026-04-13T21:44:00Z"},
	}

	prefs = map[string]obj{
		"1": {"id": "1", "theme": "dark", "language": "en", "timezone": "Europe/Berlin"},
		"2": {"id": "2", "theme": "light", "language": "ja", "timezone": "Asia/Tokyo"},
		"3": {"id": "3", "theme": "dark", "language": "fr", "timezone": "Europe/Paris"},
	}

	// eventTypes is the activity sequence the stream upstream cycles through per user.
	eventTypes = map[string][]string{
		"1": {"login", "post", "like", "comment"},
		"2": {"login", "follow", "like"},
		"3": {"login", "post", "share", "post"},
	}
)

// upstreams are the stub backends every gateway flow in testdata/ points at.
type upstreams struct {
	profile, stats, prefs, stream *httptest.Server
}

func startUpstreams() *upstreams {
	return &upstreams{
		profile: httptest.NewServer(jsonUpstream("profile", profiles, "")),
		stats:   httptest.NewServer(jsonUpstream("stats", stats, `{"status": "not_found_from_stats"}`)),
		prefs:   httptest.NewServer(jsonUpstream("prefs", prefs, "")),
		stream:  httptest.NewServer(streamUpstream()),
	}
}

func (u *upstreams) close() {
	for _, s := range []*httptest.Server{u.profile, u.stats, u.prefs, u.stream} {
		s.Close()
	}
}

// jsonUpstream serves GET /users/{id} from records, after giving inject a
// chance to override the response. notFoundBody, if set, is sent as JSON
// with the 404 for an unknown id.
func jsonUpstream(service string, records map[string]obj, notFoundBody string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		if inject(service, w, r) {
			return
		}

		rec, ok := records[r.PathValue("id")]
		if !ok {
			if notFoundBody != "" {
				w.Header().Set("Content-Type", "application/json")
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(notFoundBody))

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rec)
	})

	return mux
}

// streamUpstream serves GET /users/{id}/events as SSE, one event every
// 200ms. ?events=N closes the stream cleanly after N events, so a test can
// read a finite body; without it the stream runs until the client leaves.
func streamUpstream() http.Handler {
	const service = "stream"

	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		if inject(service, w, r) {
			return
		}

		id := r.PathValue("id")

		types, ok := eventTypes[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		limit, _ := strconv.Atoi(r.URL.Query().Get("events"))

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		rc := http.NewResponseController(w)
		_ = rc.Flush()

		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for i := 0; limit == 0 || i < limit; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				_, _ = fmt.Fprintf(w, "data: {\"type\":%q,\"user\":%q}\n\n", types[i%len(types)], id)
				_ = rc.Flush()
			}
		}
	})

	return mux
}

func inject(service string, w http.ResponseWriter, r *http.Request) bool {
	q := r.URL.Query()

	get := func(name string) string {
		if v := q.Get(service + "_" + name); v != "" {
			return v
		}

		return q.Get(name)
	}

	for _, kv := range append(q[service+"_resp_header"], q["resp_header"]...) {
		if name, value, ok := strings.Cut(kv, ":"); ok {
			w.Header().Set(name, value)
		}
	}

	if get("echo_headers") != "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]http.Header{"headers": r.Header})

		return true
	}

	if d, err := time.ParseDuration(get("delay")); err == nil {
		time.Sleep(d)
	}

	if get("drop") != "" {
		if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
			_ = conn.Close()
		}

		return true
	}

	if to := get("redirect"); to != "" {
		status := http.StatusFound
		if n, err := strconv.Atoi(get("redirect_status")); err == nil {
			status = n
		}

		http.Redirect(w, r, to, status)

		return true
	}

	if s := get("status"); s != "" {
		status, err := strconv.Atoi(s)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid status query param %q", s), http.StatusBadRequest)
			return true
		}

		body := get("body")

		if ct := get("content_type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		} else if body != "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))

		return true
	}

	return false
}
