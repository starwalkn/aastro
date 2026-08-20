package aastro

import (
	"net/http"

	"github.com/goccy/go-json"
)

// ClientResponse is an output structure that wraps the final response from the gateway to the client.
//
// The request identifier and the partial-success signal are not duplicated
// here: the former is only ever carried by the X-Request-ID response header
// (RFC 9110 leaves correlation identifiers to headers, not the body), and
// the latter is already the HTTP status code (206) - repeating it as a
// `meta.partial` boolean was a redundant second source of truth.
type ClientResponse struct {
	Data   json.RawMessage `json:"data,omitempty"`
	Errors []ClientError   `json:"errors,omitempty"`
}

type ClientError string

func (err ClientError) String() string {
	return string(err)
}

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

func WriteError(w http.ResponseWriter, code ClientError, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := ClientResponse{
		Data:   nil,
		Errors: []ClientError{code},
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// Fallback on error
		http.Error(w, http.StatusText(status), status)
	}
}
