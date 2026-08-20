package aastro

import (
	"net/http"

	"github.com/goccy/go-json"
)

// ProblemDetails is the RFC 9457 ("Problem Details for HTTP APIs") body used
// for every gateway-authored response that carries no upstream data of its
// own - rate limiting, payload limits, plugin failures, and upstream/gateway
// failures that never produced anything worth returning as data.
//
// A response that *does* carry data (a full or partial aggregation success)
// is never wrapped: the body is the aggregated payload itself, exactly as a
// client of the upstream(s) directly would see it. See Router.buildResponse.
type ProblemDetails struct {
	// Type is always "about:blank" (RFC 9457 §4.2's own default for "no
	// further-specific type"): it is never a dereferencable URI on purpose.
	// A real one - even a stable, non-existent one under the gateway's own
	// docs domain - names the software fronting this upstreams to anyone
	// who receives an error, which is itself reconnaissance: it tells a
	// caller they're behind an aggregating gateway and invites probing for
	// what that implies about the backend topology. Errors carries the
	// actual discriminator instead - a closed, generic enum that says
	// nothing about what's behind the gateway.
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
	// Errors lists every distinct underlying ClientError - always at least
	// one. This is the one machine-readable discriminator (Type is constant,
	// see above); Title is for humans and may change wording over time.
	// More than one entry only for a multi-upstream flow where several
	// upstreams failed differently. This is a problem type extension member
	// (RFC 9457 §3.2), not a spec violation.
	Errors []ClientError `json:"errors"`
}

// problemTypeBlank is RFC 9457's own placeholder for "the HTTP status code
// is all the classification there is".
const problemTypeBlank = "about:blank"

type ClientError string

const (
	ClientErrRateLimitExceeded    ClientError = "RATE_LIMIT_EXCEEDED"
	ClientErrPayloadTooLarge      ClientError = "PAYLOAD_TOO_LARGE"
	ClientErrUpstreamBodyTooLarge ClientError = "UPSTREAM_BODY_TOO_LARGE"
	ClientErrUpstreamUnavailable  ClientError = "UPSTREAM_UNAVAILABLE"
	ClientErrUpstreamError        ClientError = "UPSTREAM_ERROR"
	ClientErrUpstreamClientError  ClientError = "UPSTREAM_CLIENT_ERROR"
	ClientErrUpstreamRedirect     ClientError = "UPSTREAM_REDIRECT"
	ClientErrUpstreamMalformed    ClientError = "UPSTREAM_MALFORMED"
	ClientErrInternal             ClientError = "INTERNAL"
	ClientErrAborted              ClientError = "ABORTED"
	ClientErrValueConflict        ClientError = "VALUE_CONFLICT"
	ClientErrUnauthorized         ClientError = "UNAUTHORIZED"
)

func (err ClientError) String() string {
	return string(err)
}

// problemTitle returns the RFC 9457 Title for this error code - a short,
// human-readable summary that does not change from one occurrence to the
// next (request-specific detail, when there is any, belongs in Detail).
func (err ClientError) problemTitle() string {
	if title, ok := problemTitles[err]; ok {
		return title
	}

	return string(err)
}

var problemTitles = map[ClientError]string{
	ClientErrRateLimitExceeded:    "Rate limit exceeded",
	ClientErrPayloadTooLarge:      "Request payload too large",
	ClientErrUpstreamBodyTooLarge: "Upstream response too large",
	ClientErrUpstreamUnavailable:  "Upstream unavailable",
	ClientErrUpstreamError:        "Upstream returned a server error",
	ClientErrUpstreamClientError:  "Upstream rejected the request",
	ClientErrUpstreamRedirect:     "Upstream returned a redirect",
	ClientErrUpstreamMalformed:    "Upstream response failed gateway policy",
	ClientErrInternal:             "Internal gateway error",
	ClientErrAborted:              "Request aborted before the upstream responded",
	ClientErrValueConflict:        "Conflicting values from upstreams",
	ClientErrUnauthorized:         "Unauthorized",
}

// WriteError writes a single-cause RFC 9457 Problem Details response:
// rate limiting, request-size limits, plugin failures, and any other
// gateway-side rejection that never reaches an upstream at all.
func WriteError(w http.ResponseWriter, code ClientError, status int) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)

	problem := ProblemDetails{
		Type:   problemTypeBlank,
		Title:  code.problemTitle(),
		Status: status,
		Errors: []ClientError{code},
	}

	if err := json.NewEncoder(w).Encode(problem); err != nil {
		// Fallback on error
		http.Error(w, http.StatusText(status), status)
	}
}
