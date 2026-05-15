# Changelog

All notable changes to Kono are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions follow [Semantic Versioning](https://semver.org/).

---

## [0.4.0] - YYYY-MM-DD

### ⚠️ Breaking Changes

The configuration schema has been restructured to separate concerns by listener and
responsibility. Existing 0.3.x configs will fail to load with a clear validation error
pointing at the missing sections.

**Admin endpoints moved to a top-level `admin` section.** Previously `server.admin_port`,
`server.admin_bind_addr`, and `server.pprof` lived inside `server`. They are now grouped
under their own section, since admin is a separate listener with separate semantics
(never TLS-terminated, bound to localhost by default, distinct timeouts).

```yaml
# Before (0.3.x)
gateway:
  server:
    port: 7805
    admin_port: 9090
    admin_bind_addr: 127.0.0.1
    pprof:
      enabled: true

# After (0.4.0)
gateway:
  server:
    port: 7805
  admin:
    port: 9090
    bind_addr: 127.0.0.1
    enable_pprof: true
```

**Observability moved to a top-level `observability` section.** `metrics` and `tracing`
were previously nested under `server`. They are not server concerns - metrics is either
scraped from the admin port (Prometheus exporter) or pushed to OTLP, and tracing is
always push-only.

```yaml
# Before (0.3.x)
gateway:
  server:
    metrics: { ... }
    tracing: { ... }

# After (0.4.0)
gateway:
  observability:
    metrics: { ... }
    tracing: { ... }
```

**pprof no longer has its own port.** Previously `pprof.port` opened a separate listener.
pprof endpoints now live on the admin port under `/debug/pprof/`, controlled by
`admin.enable_pprof`. One fewer port to manage and to expose through network policy.

**Flow field `max_parallel_upstreams` renamed to `parallel_upstreams`.** Aligns docs
with actual behaviour and the code field name. Same semantics, same default
(`2 × NumCPU`).

### Added

**mTLS support, end-to-end.** Both the data port (inbound mTLS) and individual upstreams
(outbound mTLS) can now be configured with client certificate authentication, custom CA
bundles, configurable minimum TLS version (1.2 or 1.3), and SNI override.

```yaml
gateway:
  server:
    tls:
      enabled: true
      cert_file: /etc/kono/server.crt
      key_file:  /etc/kono/server.key
      client_auth: require        # none | optional | require
      client_ca_file: /etc/kono/client-ca.crt
      min_version: "1.2"

  routing:
    flows:
      - upstreams:
          - tls:
              enabled: true
              cert_file: /etc/kono/clients/users.crt
              key_file:  /etc/kono/clients/users.key
              ca_file:   /etc/kono/internal-ca.crt
              server_name: user-service.internal
```

**Liveness and readiness probes.** New `/__ready` endpoint on the admin port returns
`200` while the gateway is accepting traffic and `503` once graceful shutdown begins.
This lets Kubernetes (or any orchestrator with readiness probes) remove the pod from
service endpoints *before* the data port stops accepting connections, enabling true
zero-downtime deploys. `/__health` continues to return `200` while the process is alive
and never checks dependencies.

**Configurable admin timeout.** New `admin.timeout` field (default `5m`) controls
read/write timeout on the admin port. Replaces a previously hard-coded constant. The
generous default exists to accommodate long pprof captures (`/debug/pprof/profile`,
`/debug/pprof/trace`); production data-port timeouts remain short.

**Configurable header timeouts.** New `server.header_timeout` and `admin.header_timeout`
fields (default `5s` each) set `http.Server.ReadHeaderTimeout` on the respective listeners.
Defends against Slowloris-style attacks where a client trickles request headers slowly
to exhaust the server.

**Structured TLS handshake logging.** `http.Server.ErrorLog` is now wired through the
gateway's zap logger on both data and admin listeners. TLS handshake failures (failed
client certificate verification, version mismatch, unsupported cipher) now appear in
the same structured log stream as the rest of the application instead of escaping to
stderr via the standard logger.

### Changed

**Admin listener binds to `127.0.0.1` by default.** Previously bound to all interfaces.
The admin port carries diagnostic endpoints (`/__health`, `/__ready`, `/metrics`,
`/debug/pprof/`) that should not be exposed externally. Set `admin.bind_addr: 0.0.0.0`
explicitly if Prometheus scrapes from outside the pod network.

**`/metrics` endpoint moved to the admin port.** When `metrics.exporter: prometheus`,
the endpoint is now served on `admin.port` rather than the data port. This means
Prometheus can scrape Kono over plain HTTP without needing a client certificate, even
when the data port enforces mTLS.

**Health probe response format changed.** `/__health` now returns
`{"status": "ok"}` with `Content-Type: application/json` instead of plain text `OK`.
Consistent with the rest of the gateway's response format.

**TLS 1.0 and 1.1 are not selectable.** `min_version` accepts only `"1.2"` or `"1.3"`.
RFC 8996 deprecated 1.0 and 1.1 in 2021, and they are disabled in modern clients
regardless of server configuration.

### Migration Notes

For most users, the migration is mechanical: move `admin_port`, `admin_bind_addr`, and
`pprof` from `server` into a new top-level `admin` section, move `metrics` and `tracing`
from `server` into a new `observability` section. If you relied on `metrics` being on
the data port - point Prometheus at the admin port instead.

If you ran `pprof` on a separate port: that port can be closed, pprof now lives on the
admin port. Update any internal documentation or scrape configs.

If `max_parallel_upstreams` appears anywhere in your configs: rename to
`parallel_upstreams`. Behaviour is unchanged.

If you used `min_version: "1.0"` or `"1.1"` anywhere: bump to `"1.2"`. Configuration
loading will refuse to start with the old values.

### Fixed

**WaitGroup pooling in scatter.** Removed `sync.Pool` reuse of `sync.WaitGroup` values.
WaitGroup does not reset to a clean state after use, and pooling it risks counter
corruption on edge paths. Allocation cost of a fresh WaitGroup per request is negligible.

**Race on shutdown error reporting.** Fixed a data race in the serve command where the
listener error and the main goroutine's shutdown path both wrote to the same `err`
variable. Local variable inside the goroutine now passes the error exclusively through
the channel.

### Added

- Add `WWW-Authenticate` header to auth middleware

### Changed

- Golang build version ldflags will now automatically come from the docker meta step outputs
- Sliding window rate limiter instead of fixed-window

### Fixed

- Added missing masker plugin and cors middleware to image

## [0.3.0] - 2026-05-04

### Added

- Distributed tracing via OpenTelemetry OTLP/HTTP (`gateway.server.tracing` config block)
- Service identity via `gateway.service.name` config and `-ldflags "-X main.version=…"` injection
- W3C TraceContext + Baggage propagation, installed unconditionally
- `X-Request-Fingerprint` response header and `kono.request.fingerprint` span attribute for correlation across
  observability channels
- `kono plugin init` CLI command to create a plugin or middleware skeleton

### Changed

- The gateway in docker container is now running as a non-root user
- sdk.Plugin.Init() should now return an error
- Used [Ginkgo](https://github.com/onsi/ginkgo) for tests

### Fixed

- Passthrough flows no longer broken by `client.Timeout` (separate streamClient without timeout)
- Passthrough flows no longer broken by `http.Server.WriteTimeout` (per-request
  `ResponseController.SetWriteDeadline(time.Time{})`)
- Client disconnect during passthrough no longer logged as upstream error

---

## [0.2.0] — 2026-04-26

### Added

- **Passthrough mode** - new `passthrough: true` flow option proxies requests directly to a single upstream without
  buffering or aggregation. Designed for Server-Sent Events (SSE), chunked transfer, and any long-lived HTTP connection.
  Request plugins still run; response plugins are skipped.
- **OTLP metrics exporter** - metrics can now be pushed to any OpenTelemetry-compatible backend via `exporter: otlp`.
  Previously only Prometheus pull-mode was supported.
- **Namespace aggregation strategy** — new `strategy: namespace` places each upstream response under its name as a key:
  `{"profile": {...}, "stats": {...}}`.
- **Response meta envelope** - all gateway responses now include a `meta` object with `request_id` (ULID) and `partial`
  flag alongside `data` and `errors`.
- **`kono viz` command** - CLI command that renders a visual tree of all configured flows and upstreams using terminal
  color output.
- **JWKS background refresh** - the `auth` middleware now refreshes JWKS keys in the background on a configurable
  interval (`jwks_refresh_interval`, default 5m), reducing on-demand refresh latency during key rotation.
- **`sdk.Closer` interface** - middlewares that hold background resources can implement `Close() error`. Kono calls it
  on shutdown via `Router.Close()`.
- **Configuration defaults via struct tags** - upstream timeouts, transport pool settings, and server timeout now use
  `default:` tags powered by `creasty/defaults`, eliminating the manual `ensureGatewayDefaults` function.
- **`kono validate` command** - validates the configuration file and reports human-readable field-level errors without
  starting the gateway.
- **Full hop-by-hop header filtering** - `Keep-Alive`, `TE`, `Proxy-Authenticate`, `Proxy-Authorization`, and `Upgrade`
  are now stripped from proxied responses in addition to the previously filtered headers.
- **`cors` built-in middleware** - Cross-Origin Resource Sharing support with configurable origins, methods, headers,
  credentials, and preflight cache.
- **`ClientErrAborted` error code** - new `ABORTED` error code and `503 Service Unavailable` status for requests
  cancelled by the client before completion.

### Changed

- **`dispatcher` renamed to `scatter`** - internal component renamed to better reflect the fan-out pattern. No
  user-facing configuration change.
- **Upstream policy validation moved into upstream** — `requireBody` and `allowedStatuses` policy checks now run inside
  `upstream.call()` after the circuit breaker update, so policy violations do not affect circuit breaker state.
- **`aggregation` is now optional for passthrough flows** — the `aggregation` block may be omitted when
  `passthrough: true`. Configuration validation enforces this.
- **`pprof.port` only required when pprof is enabled** — previously the validator required a port value unconditionally.
- **`AggregationConfig` is now a pointer in `FlowConfig`** — allows the validator to correctly apply
  `required_if=Passthrough false`.
- **`Router.flows` and all internal types unexported** — `Flow`, `Upstream`, `AggregatedResponse`, `UpstreamResponse`,
  `UpstreamError`, and related types are no longer exported. Public API is limited to `Router`, `NewRouter`,
  `RoutingConfigSet`, config types, `LoadConfig`, `ClientError`, `ClientResponse`, and `WriteError`.

### Fixed

- `passthrough` field not being set on compiled `flow` struct — passthrough flows were silently falling back to buffered
  mode.
- `trackingWriter` not forwarding `Flush()` — SSE events were buffered until connection close instead of being flushed
  after each chunk.
- `headersAlreadySent` always returning `true` — used `http.Flusher` type assertion which is satisfied by almost any
  `ResponseWriter`, making the double-write guard ineffective.
- `Content-Length` forwarded from upstream in passthrough mode — conflicted with chunked/streaming responses and caused
  client parsing errors.
- Circuit breaker state not updated correctly on `HalfOpen` success and `Open` failure — missing `case` branches in
  switch statements.

---

## [0.1.0] — initial release

- Core routing with chi
- Fan-out dispatch to multiple upstreams
- `merge` and `array` aggregation strategies
- Circuit breaker, retry, load balancing per upstream
- Prometheus metrics
- Plugin and middleware `.so` loading
- Built-in plugins: `camelify`, `snakeify`, `masker`
- Built-in middlewares: `auth`, `logger`, `recoverer`, `compressor`
- `trusted_proxies` and per-IP rate limiting
- YAML configuration with validation