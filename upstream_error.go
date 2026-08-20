package aastro

import "fmt"

// upstreamErrorKind classifies why an upstream call did not produce a clean
// success. It is the single raw signal produced by httpUpstream (see
// classifyStatus/classifyDoError in upstream.go) and consumed everywhere
// else through kindProps below.
type upstreamErrorKind string

const (
	upstreamTimeout         upstreamErrorKind = "timeout"
	upstreamCanceled        upstreamErrorKind = "canceled"
	upstreamConnection      upstreamErrorKind = "connection"
	upstreamRedirect        upstreamErrorKind = "redirect"     // 3XX
	upstreamClientError     upstreamErrorKind = "client_error" // 4XX
	upstreamBadStatus       upstreamErrorKind = "bad_status"   // 5XX
	upstreamReadError       upstreamErrorKind = "read_error"
	upstreamBodyTooLarge    upstreamErrorKind = "body_too_large"
	upstreamCircuitOpen     upstreamErrorKind = "circuit_open"
	upstreamInternal        upstreamErrorKind = "internal"
	upstreamPolicyViolation upstreamErrorKind = "policy_violation"
)

type upstreamError struct {
	kind upstreamErrorKind
	err  error
}

func (ue *upstreamError) Error() string { return string(ue.kind) }
func (ue *upstreamError) Unwrap() error {
	if ue == nil {
		return nil
	}

	return ue.err
}

// breakerOutcome is what a call result says about the upstream's health,
// from the circuit breaker's point of view.
type breakerOutcome uint8

const (
	breakerNoSignal breakerOutcome = iota
	breakerSuccess
	breakerFailure
)

// retryEligibility classifies how a kind participates in the upstream's
// retry policy, independent of any specific status/config check.
type retryEligibility uint8

const (
	retryNever retryEligibility = iota
	retryAlways
	retryByStatus
)

// kindProps captures every fact about an upstreamErrorKind that the retry,
// circuit breaker, policy, and response layers need. Previously each of
// those layers re-derived these facts independently via its own switch
// statement over upstreamErrorKind — four separate places to keep in sync
// by hand (see CHANGELOG 0.9.0, where splitting UPSTREAM_CLIENT_ERROR from
// UPSTREAM_REDIRECT meant touching several of them). This table is the
// single source of truth instead; init() below panics at startup if a kind
// is missing an entry, so a forgotten update fails loudly instead of
// silently misclassifying one layer.
type kindProps struct {
	breaker breakerOutcome
	retry   retryEligibility

	// hasBody reports whether the upstream actually produced a status+body
	// worth checking upstreamPolicy.requireBody against.
	hasBody bool

	// propagatable reports whether this is an answer FROM the upstream (its
	// own status/headers/body) that a single-upstream flow should forward
	// to the client as-is, rather than translate into a gateway error.
	propagatable bool

	// clientErr is the code reported in the JSON error envelope when the
	// response is not propagated.
	clientErr ClientError
}

var kindTable = map[upstreamErrorKind]kindProps{
	upstreamTimeout:    {breaker: breakerFailure, retry: retryAlways, clientErr: ClientErrUpstreamUnavailable},
	upstreamConnection: {breaker: breakerFailure, retry: retryAlways, clientErr: ClientErrUpstreamUnavailable},

	upstreamCanceled:    {breaker: breakerNoSignal, retry: retryNever, clientErr: ClientErrAborted},
	upstreamCircuitOpen: {breaker: breakerNoSignal, retry: retryNever, clientErr: ClientErrUpstreamUnavailable},
	upstreamInternal:    {breaker: breakerNoSignal, retry: retryNever, clientErr: ClientErrInternal},

	upstreamReadError:    {breaker: breakerFailure, retry: retryNever, clientErr: ClientErrInternal},
	upstreamBodyTooLarge: {breaker: breakerSuccess, retry: retryNever, clientErr: ClientErrUpstreamBodyTooLarge},

	upstreamBadStatus: {breaker: breakerFailure, retry: retryByStatus, hasBody: true, clientErr: ClientErrUpstreamError},

	upstreamClientError: {
		breaker: breakerSuccess, retry: retryByStatus, hasBody: true,
		propagatable: true, clientErr: ClientErrUpstreamClientError,
	},
	upstreamRedirect: {
		breaker: breakerSuccess, retry: retryNever, hasBody: true,
		propagatable: true, clientErr: ClientErrUpstreamRedirect,
	},

	upstreamPolicyViolation: {breaker: breakerSuccess, retry: retryNever, clientErr: ClientErrUpstreamMalformed},
}

// allUpstreamErrorKinds is used only to make kindTable's completeness
// checkable — both at startup (init below) and from tests.
var allUpstreamErrorKinds = []upstreamErrorKind{
	upstreamTimeout, upstreamCanceled, upstreamConnection, upstreamRedirect,
	upstreamClientError, upstreamBadStatus, upstreamReadError, upstreamBodyTooLarge,
	upstreamCircuitOpen, upstreamInternal, upstreamPolicyViolation,
}

func init() {
	for _, k := range allUpstreamErrorKinds {
		if _, ok := kindTable[k]; !ok {
			panic(fmt.Sprintf("aastro: upstreamErrorKind %q has no kindProps entry in kindTable", k))
		}
	}
}

// unknownKindProps is returned for a kind absent from kindTable. This can
// only happen for a malformed *upstreamError built outside this package
// (e.g. by a test double or a future caller) — every kind this package
// itself produces is checked exhaustive by init() above. It mirrors what
// mapUpstreamError used to fall back to before this table existed: treat it
// as an internal error, and give the breaker/retry logic no signal to act on.
var unknownKindProps = kindProps{breaker: breakerNoSignal, retry: retryNever, clientErr: ClientErrInternal}

func (k upstreamErrorKind) props() kindProps {
	if p, ok := kindTable[k]; ok {
		return p
	}

	return unknownKindProps
}

// breakerOutcomeFor answers one question: what does this result say about the
// upstream's health? A complete HTTP response - whatever its status - proves
// the upstream is alive; a transport failure proves it is not; anything that
// never left the gateway proves nothing.
func breakerOutcomeFor(uerr *upstreamError) breakerOutcome {
	if uerr == nil {
		return breakerSuccess
	}

	return uerr.kind.props().breaker
}

// isStatusFailure reports whether kind carries a status+body actually
// produced by the upstream, i.e. whether response-body policy checks
// (requireBody, ...) apply to it.
func isStatusFailure(kind upstreamErrorKind) bool {
	return kind.props().hasBody
}

// isPropagatable reports whether kind represents an answer from the upstream
// that a single-upstream flow should forward to the client as-is, rather
// than translate into a gateway error response.
func isPropagatable(kind upstreamErrorKind) bool {
	return kind.props().propagatable
}

// mapUpstreamError translates an upstream-facing error into the client-facing
// error code reported in the JSON error envelope.
func mapUpstreamError(err *upstreamError) ClientError {
	if err == nil {
		return ClientErrInternal
	}

	return err.kind.props().clientErr
}
