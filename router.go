package aastro

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/goccy/go-json"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/starwalkn/aastro/internal/metric"
	"github.com/starwalkn/aastro/internal/ratelimit"
	"github.com/starwalkn/aastro/internal/tracing"
	"github.com/starwalkn/aastro/sdk"
)

// Keys must be net/textproto's canonical form (net/http always stores and
// iterates response headers that way) - "TE" is the one entry here that
// doesn't look canonical at a glance: textproto.CanonicalMIMEHeaderKey("TE")
// is "Te" (no hyphen to separate words, so only the first letter is upper-cased),
// not "TE". Written any other way, this lookup silently never
// matches and the header leaks to the client.
var hopByHopHeaders = map[string]struct{}{
	"Content-Length":      {},
	"Transfer-Encoding":   {},
	"Connection":          {},
	"Trailer":             {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Upgrade":             {},
}

var fingerprintIgnoredHeaders = map[string]struct{}{
	"User-Agent":        {},
	"Cookie":            {},
	"Authorization":     {},
	"X-Request-Id":      {},
	"X-Forwarded-For":   {},
	"X-Forwarded-Proto": {},
	"X-Forwarded-Host":  {},
	"X-Forwarded-Port":  {},
	"X-Real-Ip":         {},
	"Forwarded":         {},
	"Host":              {},
	"Accept-Encoding":   {},
	"Accept-Language":   {},
}

type Router struct {
	chiRouter  *chi.Mux
	scatter    scatter
	aggregator aggregator
	flows      []flow

	trustedProxies []*net.IPNet

	log         *zap.Logger
	metrics     *metric.Metrics
	rateLimiter *ratelimit.RateLimit
}

// ServeHTTP handles incoming HTTP requests through the full router pipeline:
//  1. Rate limiting - rejects requests exceeding the configured limit.
//  2. Flow matching - chi router finds the flow by method and path (404 if none).
//  3. Middleware execution - per-flow middlewares wrap the handler.
//  4. Request plugins - run before the upstream call; may modify the request.
//  5. Upstream dispatch - a streaming flow is piped through unbuffered
//     (handleStreaming); a single-upstream flow is called directly and its
//     status/headers/body are proxied as-is (buildProxyResponse); a
//     multi-upstream flow fans out and aggregates (merge/array/namespace,
//     with bestEffort support).
//  6. Response plugins - run after dispatch; may modify headers or body.
//  7. Response writing - status, headers, and body sent to the client.
//
// A single-upstream flow forwards the upstream's own status/body verbatim on
// success or on a client error/redirect. A multi-upstream flow's success or
// partial-success (206, bestEffort) body is the aggregated data itself, with
// no gateway-added wrapper - a client never has to unwrap a response to get
// at the payload. Only a response with no data at all (a hard failure with
// nothing to aggregate, a rejected request that never reached an upstream)
// is a body, and that body is an RFC 9457 Problem Details document
// (application/problem+json), not a bespoke shape. Status codes: 200 on full
// success, 206 on partial, 502/500 on failure.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.metrics.IncRequestsInFlight()
	defer r.metrics.DecRequestsInFlight()

	tracer := otel.Tracer(tracing.TracerName)

	ctx := otel.GetTextMapPropagator().Extract(req.Context(), propagation.HeaderCarrier(req.Header))
	ctx, span := tracer.Start(ctx, "aastro.request",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("url.path", req.URL.Path),
		),
	)
	defer span.End()

	clientIP := extractClientIP(req, r.trustedProxies)
	ctx = withClientIP(ctx, clientIP)
	req = req.WithContext(ctx)

	if !r.allowRequest(w, clientIP) {
		span.SetAttributes(attribute.Int("http.status_code", http.StatusTooManyRequests))
		span.SetStatus(codes.Error, "rate limited")

		return
	}

	r.chiRouter.ServeHTTP(w, req)
}

func (r *Router) Close() error {
	for i := range r.flows {
		for _, mw := range r.flows[i].middlewares {
			if c, ok := mw.(sdk.Closer); ok {
				if err := c.Close(); err != nil {
					r.log.Error("middleware close failed",
						zap.String("name", mw.Name()),
						zap.Error(err),
					)
				}
			}
		}
	}

	return nil
}

func (r *Router) allowRequest(w http.ResponseWriter, clientIP string) bool {
	if r.rateLimiter == nil {
		return true
	}

	if r.rateLimiter.Allow(clientIP) {
		return true
	}

	r.metrics.IncFailedRequestsTotal(metric.FailReasonTooManyRequests)
	WriteError(w, ClientErrRateLimitExceeded, http.StatusTooManyRequests)

	return false
}

func (r *Router) newFlowHandler(f *flow) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		defer r.metrics.UpdateRequestsDuration(f.path, f.method, start)

		span := trace.SpanFromContext(req.Context())
		span.SetAttributes(attribute.String("http.route", f.path))

		requestID := getOrCreateRequestID(req)
		fingerprint := computeFingerprint(req, f.path)

		ctx := req.Context()
		ctx = withRoute(withRequestID(withFingerprint(ctx, fingerprint), requestID), f.path)

		req = req.WithContext(ctx)

		span.SetAttributes(
			attribute.String("aastro.request.id", requestID),
			attribute.String("aastro.request.fingerprint", fingerprint),
		)

		log := r.log.With(
			zap.String("request_id", requestID),
			zap.String("fingerprint", fingerprint),
		)

		if f.streaming {
			r.handleStreaming(w, req, f, log)
			return
		}

		actx := newContext(req)

		if !r.executePlugins(sdk.PluginTypeRequest, w, actx, f, log) {
			return
		}

		httpResp, ok := r.dispatch(w, req, f, log)
		if !ok {
			return
		}
		defer func() { _ = httpResp.Body.Close() }()

		actx.SetResponse(httpResp)

		if !r.executePlugins(sdk.PluginTypeResponse, w, actx, f, log) {
			return
		}

		finalResp := actx.Response()
		if finalResp != nil && finalResp != httpResp && finalResp.Body != nil {
			defer func() { _ = finalResp.Body.Close() }()
		}

		if finalResp.Body != nil {
			bodyBytes, _ := io.ReadAll(finalResp.Body)
			finalResp.ContentLength = int64(len(bodyBytes))
			finalResp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		w.Header().Set("Content-Length", strconv.Itoa(int(finalResp.ContentLength)))

		r.metrics.IncRequestsTotal(f.path, req.Method, finalResp.StatusCode)
		span.SetAttributes(attribute.Int("http.status_code", finalResp.StatusCode))
		if finalResp.StatusCode >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(finalResp.StatusCode))
		}

		r.copyResponse(w, finalResp)
	})
}

// executePlugins runs all plugins of the given type in order.
// On the first plugin error it writes a 500 to w and returns false -
// the caller must treat false as "response already sent, stop processing".
func (r *Router) executePlugins(pluginType sdk.PluginType, w http.ResponseWriter, actx sdk.Context, f *flow, log *zap.Logger) bool {
	tracer := otel.Tracer(tracing.TracerName)

	for _, p := range f.plugins {
		if p.Type() != pluginType {
			continue
		}

		log.Debug("executing plugin",
			zap.String("type", pluginType.String()),
			zap.String("name", p.Info().Name),
		)

		ctx, span := tracer.Start(actx.Request().Context(), "aastro.plugin",
			trace.WithAttributes(
				attribute.String("aastro.plugin.name", p.Info().Name),
				attribute.String("aastro.plugin.type", pluginType.String()),
			),
		)

		actx.SetRequest(actx.Request().WithContext(ctx))
		err := p.Execute(actx)
		span.End()

		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "plugin execution failed")

			log.Error("plugin execution failed",
				zap.String("type", pluginType.String()),
				zap.String("name", p.Info().Name),
				zap.Error(err),
			)
			WriteError(w, ClientErrInternal, http.StatusInternalServerError)

			return false
		}
	}

	return true
}

// dispatch calls the flow's upstream(s) and builds the resulting HTTP
// response. A single-upstream flow bypasses the scatter/aggregate machinery
// entirely and is proxied directly (buildProxyResponse); a multi-upstream
// flow fans out and aggregates as before (buildResponse). Returns ok=false
// when it has already written an error response to w - the caller must
// treat that as "response already sent, stop processing".
func (r *Router) dispatch(w http.ResponseWriter, req *http.Request, f *flow, log *zap.Logger) (*http.Response, bool) {
	if len(f.upstreams) == 1 {
		resp, ok := r.scatter.call(f, req)
		if !ok {
			r.log.Error("request body too large", zap.Int("max_body_size", maxBodySize))
			WriteError(w, ClientErrPayloadTooLarge, http.StatusRequestEntityTooLarge)

			return nil, false
		}

		return r.buildProxyResponse(req.Context(), resp, log), true
	}

	upstreamResponses := r.scatter.scatter(f, req)
	if upstreamResponses == nil {
		r.log.Error("request body too large", zap.Int("max_body_size", maxBodySize))
		WriteError(w, ClientErrPayloadTooLarge, http.StatusRequestEntityTooLarge)

		return nil, false
	}

	if r.log.Core().Enabled(zap.DebugLevel) {
		var ok, failed int

		for _, resp := range upstreamResponses {
			if resp.err != nil {
				failed++
			} else {
				ok++
			}
		}

		r.log.Debug("scatter finished",
			zap.Int("ok", ok),
			zap.Int("failed", failed),
		)
	}

	return r.buildResponse(req.Context(), upstreamResponses, f, log), true
}

// buildProxyResponse builds the HTTP response for a single-upstream flow.
// A success or a client-error/redirect answer from the upstream (its status,
// headers, and body) is forwarded to the client as-is. Any other failure -
// the upstream never answered, answered with a 5xx, or failed gateway
// policy - has no upstream body worth forwarding and is reported as an
// RFC 9457 Problem Details document instead.
func (r *Router) buildProxyResponse(ctx context.Context, resp upstreamResponse, log *zap.Logger) *http.Response {
	requestID := requestIDFromContext(ctx)
	fingerprint := fingerprintFromContext(ctx)

	headers := resp.headers
	if headers == nil {
		headers = make(http.Header)
	}

	headers.Set("X-Request-ID", requestID)
	headers.Set("X-Request-Fingerprint", fingerprint)

	if resp.err == nil || isPropagatable(resp.err.kind) {
		if headers.Get("Content-Type") == "" {
			headers.Set("Content-Type", "application/json; charset=utf-8")
		}

		log.Debug("proxying upstream response",
			zap.Int("status", resp.status),
			zap.Int("body_bytes", len(resp.body)),
		)

		return &http.Response{
			Status:        fmt.Sprintf("%d %s", resp.status, http.StatusText(resp.status)),
			StatusCode:    resp.status,
			ContentLength: int64(len(resp.body)),
			Body:          io.NopCloser(bytes.NewReader(resp.body)),
			Header:        headers,
		}
	}

	clientErr := mapUpstreamError(resp.err)
	status := r.statusFromErrors([]ClientError{clientErr}, false)

	log.Warn("upstream error, returning a problem details response",
		zap.String("upstream_error", resp.err.Unwrap().Error()),
		zap.String("client_error", clientErr.String()),
	)

	headers.Set("Content-Type", "application/problem+json")

	body := mustMarshal(problemForErrors([]ClientError{clientErr}, status))

	return &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		ContentLength: int64(len(body)),
		Body:          io.NopCloser(bytes.NewBuffer(body)),
		Header:        headers,
	}
}

func (r *Router) buildResponse(ctx context.Context, upstreamResponses []upstreamResponse, f *flow, log *zap.Logger) *http.Response {
	aggregated := r.aggregator.aggregate(f.upstreams, upstreamResponses, f.aggregation, log.Named("aggregated"))

	headers := aggregated.headers
	if headers == nil {
		headers = make(http.Header)
	}

	requestID := requestIDFromContext(ctx)
	fingerprint := fingerprintFromContext(ctx)

	headers.Set("X-Request-ID", requestID)
	headers.Set("X-Request-Fingerprint", fingerprint)

	log.Debug("aggregation finished",
		zap.String("strategy", f.aggregation.strategy.String()),
		zap.Int("data_bytes", len(aggregated.data)),
		zap.Int("errors", len(aggregated.errors)),
		zap.Bool("partial", aggregated.partial),
	)

	status := r.resolveStatus(aggregated)
	body := r.buildResponseBody(headers, aggregated, status)

	return &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		ContentLength: int64(len(body)),
		Body:          io.NopCloser(bytes.NewBuffer(body)),
		Header:        headers,
	}
}

// buildResponseBody sets the outgoing Content-Type on headers and returns
// the body. A full failure (no upstream produced anything) is reported as
// an RFC 9457 Problem Details document; a full or partial success returns
// the aggregated data verbatim, with the same shape either way - which
// upstreams failed on a partial success goes on the X-Partial-Errors
// header (one value per failure) instead of a body field, so a client never
// has to branch on response shape depending on whether every upstream
// happened to succeed.
func (r *Router) buildResponseBody(headers http.Header, aggregated aggregatedResponse, status int) []byte {
	// RFC 9110
	if status == http.StatusNoContent || status == http.StatusNotModified {
		return nil
	}

	if len(aggregated.errors) > 0 && !aggregated.partial {
		headers.Set("Content-Type", "application/problem+json")
		return mustMarshal(problemForErrors(aggregated.errors, status))
	}

	headers.Set("Content-Type", "application/json; charset=utf-8")

	if aggregated.partial {
		for _, e := range aggregated.errors {
			headers.Add("X-Partial-Errors", e.String())
		}
	}

	return aggregated.data
}

func (r *Router) copyResponse(w http.ResponseWriter, resp *http.Response) {
	for k, vv := range resp.Header {
		if _, skip := hopByHopHeaders[k]; skip {
			continue
		}

		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	w.WriteHeader(resp.StatusCode)

	if resp.Body != nil {
		_, _ = io.Copy(w, resp.Body)
	}
}

func (r *Router) resolveStatus(agg aggregatedResponse) int {
	if agg.partial {
		return http.StatusPartialContent
	}

	if agg.status != 0 {
		return agg.status
	}

	return r.statusFromErrors(agg.errors, false)
}

// statusFromErrors maps aggregation errors to the most appropriate HTTP status code.
// partial takes precedence: even with errors, 206 signals a partial success.
func (r *Router) statusFromErrors(errors []ClientError, partial bool) int {
	if partial {
		return http.StatusPartialContent
	}

	if len(errors) == 0 {
		return http.StatusOK
	}

	return statusForError(selectPrimaryError(errors))
}

// ── Package-level helpers ─────────────────────────────────────────────────────

// selectPrimaryError picks the single most specific error out of a set,
// by priority (see errorPriority) - used both for the HTTP status (below)
// and for a multi-error Problem Details document's Type/Title (problemForErrors),
// so the two always agree on which failure is "the" failure.
func selectPrimaryError(errors []ClientError) ClientError {
	var selected ClientError

	maxPriority := -1

	for _, e := range errors {
		if p := errorPriority(e); p > maxPriority {
			maxPriority = p
			selected = e
		}
	}

	return selected
}

func statusForError(e ClientError) int {
	switch e {
	case ClientErrRateLimitExceeded:
		return http.StatusTooManyRequests
	case ClientErrPayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case ClientErrUpstreamBodyTooLarge, ClientErrUpstreamUnavailable, ClientErrUpstreamError,
		ClientErrUpstreamMalformed, ClientErrUpstreamClientError, ClientErrUpstreamRedirect:

		return http.StatusBadGateway
	case ClientErrValueConflict:
		return http.StatusConflict
	case ClientErrAborted:
		// Client disconnected before the upstream responded; there is nothing
		// meaningful to send back, but we still need a status for logging.
		return http.StatusServiceUnavailable
	case ClientErrInternal:
		return http.StatusInternalServerError
	case ClientErrUnauthorized:
		return http.StatusUnauthorized
	}

	return http.StatusInternalServerError
}

// problemForErrors builds the RFC 9457 body for a response with one or more
// failing causes. Title summarizes the single highest-priority error (Type
// is always "about:blank", see ProblemDetails); Errors always carries the
// full set, even when it's only the one - it's the sole machine-readable
// discriminator now that Type doesn't vary.
func problemForErrors(errors []ClientError, status int) ProblemDetails {
	return ProblemDetails{
		Type:   problemTypeBlank,
		Title:  selectPrimaryError(errors).problemTitle(),
		Status: status,
		Errors: errors,
	}
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"type":"about:blank","title":"Internal gateway error","status":500,"errors":["INTERNAL"]}`)
	}

	return b
}

// extractClientIP resolves the real client IP following the chain:
// X-Forwarded-For → X-Real-IP → RemoteAddr.
func extractClientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	peer := remoteIP(r)

	if peer == nil || !ipInNets(peer, trustedProxies) {
		return peerString(r, peer)
	}

	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")

		for i := len(parts) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(parts[i]))
			if ip == nil {
				break
			}

			if !ipInNets(ip, trustedProxies) {
				return ip.String()
			}
		}
	}

	if xrip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); xrip != nil {
		return xrip.String()
	}

	return peerString(r, peer)
}

func remoteIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	return net.ParseIP(host)
}

func peerString(r *http.Request, peer net.IP) string {
	if peer != nil {
		return peer.String()
	}

	return r.RemoteAddr
}

func ipInNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}

	return false
}

func getOrCreateRequestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}

	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}

	return id.String()
}

func computeFingerprint(r *http.Request, flowPath string) string {
	var b strings.Builder

	b.WriteString(r.Method)
	b.WriteByte('|')
	b.WriteString(flowPath)
	b.WriteByte('|')

	headers := make([]string, 0, len(r.Header))
	for name := range r.Header {
		if _, skip := fingerprintIgnoredHeaders[name]; skip {
			continue
		}

		headers = append(headers, name)
	}

	sort.Strings(headers)
	b.WriteString(strings.Join(headers, ","))
	b.WriteByte('|')

	if r.URL.RawQuery != "" {
		qs := r.URL.Query()

		queries := make([]string, 0, len(qs))
		for name := range qs {
			queries = append(queries, name)
		}

		sort.Strings(queries)
		b.WriteString(strings.Join(queries, ","))
	}

	sum := sha256.Sum256([]byte(b.String()))

	return hex.EncodeToString(sum[:8])
}

const (
	errPriorityRateLimit    = 100
	errPriorityUnauthorized = 95
	errPriorityPayloadSize  = 90
	errPriorityConflict     = 80
	errPriorityUpstream     = 50
	errPriorityInternal     = 10
)

// clientErrorPriority determines which error code wins when multiple are present.
// Higher value = more specific HTTP status code returned.
func errorPriority(e ClientError) int {
	switch e {
	case ClientErrRateLimitExceeded:
		return errPriorityRateLimit
	case ClientErrUnauthorized:
		return errPriorityUnauthorized
	case ClientErrPayloadTooLarge, ClientErrUpstreamBodyTooLarge:
		return errPriorityPayloadSize
	case ClientErrValueConflict:
		return errPriorityConflict
	case ClientErrUpstreamUnavailable, ClientErrUpstreamError, ClientErrUpstreamMalformed,
		ClientErrAborted, ClientErrUpstreamClientError, ClientErrUpstreamRedirect:

		return errPriorityUpstream
	case ClientErrInternal:
		return errPriorityInternal
	}

	return 0
}
