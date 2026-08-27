# Aastro API Gateway

A lightweight, modular, high-performance **API Gateway** for modern microservices -
parallel fan-out, declarative response aggregation, and `.so` plugins, configured in YAML.

[![Go Version](https://img.shields.io/github/go-mod/go-version/starwalkn/aastro)](https://golang.org)
[![License](https://img.shields.io/github/license/starwalkn/aastro)](LICENSE)
[![GitHub release](https://img.shields.io/github/v/release/starwalkn/aastro)](https://github.com/starwalkn/aastro/releases)
[![Docker Pulls](https://img.shields.io/docker/pulls/starwalkn/aastro)](https://hub.docker.com/r/starwalkn/aastro)
[![Coverage Status](https://coveralls.io/repos/github/starwalkn/aastro/badge.svg)](https://coveralls.io/github/starwalkn/aastro)

**[Documentation](https://starwalkn.github.io/aastro-docs/)** ·
[Configuration reference](sample.config.yaml) ·
[Changelog](CHANGELOG.md) ·
[Discussions](https://github.com/starwalkn/aastro/discussions)

> **Status: 0.x.** The gateway is used in real deployments and every release is documented,
> but the configuration schema is still allowed to change in minor versions. Breaking changes
> are always called out in the [changelog](CHANGELOG.md) with a migration note. Pin an exact
> image tag.

---

## What it does

One HTTP request in, several upstream calls out in parallel, one response back - merged,
namespaced, or arrayed, with per-upstream timeouts, retries, and circuit breakers. Everything
is described in a YAML file; no code is required to add a route.

```yaml
# aastro.yaml - a complete, minimal configuration
schema: v1

gateway:
  server:
    port: 7805
  admin:
    port: 9090
    bind_addr: 0.0.0.0        # 127.0.0.1 by default; open it up inside a container

  routing:
    flows:
      - path: /api/v1/customers/{customer_id}
        method: GET

        aggregation:
          strategy: merge     # merge | array | namespace
          best_effort: true   # answer with partial data instead of failing

        upstreams:
          - name: users
            hosts: https://users.internal
            path: /v1/users/{customer_id}
            forward_params: ["customer_id"]
            timeout: 3s

          - name: orders
            hosts: https://orders.internal
            path: /v1/customers/{customer_id}/orders
            forward_params: ["customer_id"]
            timeout: 2s
            policy:
              retry:
                max_retries: 2
                retry_on_statuses: [502, 503, 504]
                backoff_delay: 100ms
```

```console
$ curl -s localhost:7805/api/v1/customers/42
```

```json
{
  "id": "42",
  "email": "ada@example.com",
  "orders": [
    { 
      "id": "9001", 
      "total": 1200
    }
  ]
}
```

If `orders` is down and `best_effort` is on, the client gets `206 Partial Content` with the
`X-Partial-Errors` header:

```bash
HTTP/1.1 206 Partial Content
X-Request-ID: 018f4a2b-7c3d-7e4f-a5b6-c7d8e9f0a1b2
X-Request-Fingerprint: 3f9a1c2b7e4d5061
X-Partial-Errors: UPSTREAM_UNAVAILABLE
Content-Type: application/json; charset=utf-8

{
  "data": {
    "id": "42",
    "email": "ada@example.com"
  }
}
```

Every configuration option, with comments, lives in [`sample.config.yaml`](sample.config.yaml).

---

## Quick start

### Docker

```bash
docker run \
  -p 7805:7805 \
  -v "$(pwd)/aastro.yaml:/etc/aastro/config.yaml:ro" \
  starwalkn/aastro:latest
```

`/etc/aastro/config.yaml` is the default config path. To mount it elsewhere, point
`AASTRO_CONFIG` at it or pass `-c /path/to/config.yaml` after the image name.

### From source

Building the gateway requires `CGO_ENABLED=1` and a C toolchain, because plugins are Go shared
objects (`-buildmode=plugin`).

```bash
git clone https://github.com/starwalkn/aastro.git
cd aastro

make all GOOS=<YOUR_OS> GOARCH=<YOUR_ARCH>   # builds .bin/aastro, .bin/aastroctl and the builtin .so files

./.bin/aastro -c aastro.yaml
```

### Validate before you deploy

```bash
aastro -t -c aastro.yaml    # parse + validate, exit non-zero on error
aastro -T -c aastro.yaml    # same, plus dump the effective config (defaults applied) to stdout
```

---

## Features

**Routing & aggregation**
- Parallel fan-out to any number of upstreams, bounded by `parallel_upstreams`
- `merge`, `array`, and `namespace` aggregation strategies with configurable conflict policy
- Best-effort mode: `206 Partial Content` instead of an all-or-nothing failure
- Streaming flows for SSE, chunked, and long-lived streaming responses
- Path parameter extraction and forwarding; header and query allow-lists

**Resilience**
- Retries with an idempotency guard - non-idempotent methods are never replayed
- Circuit breaker per upstream, with state exported as a Prometheus metric
- Load balancing across hosts: `round_robin` or `least_conns`
- Per-IP sliding-window rate limiting with trusted-proxy-aware client IP resolution
- Response size limits, status allow-lists, header blacklists

**Security**
- TLS and mutual TLS on the inbound data port and per upstream
- Zero-downtime certificate hot-reload - cert-manager, Vault Agent, and SPIFFE/SPIRE ready
- Builtin JWT `auth` middleware
- Admin port bound to localhost by default and never TLS-terminated

**Observability**
- Prometheus exporter or OTLP push for metrics
- Distributed tracing over OTLP with W3C Trace Context and Baggage propagation
- `X-Request-Fingerprint` correlation across logs, metrics, and traces
- `/__health`, `/__ready`, `/metrics`, and optional `/debug/pprof/` on a separate admin port

**Extensibility**
- Request- and response-phase plugins, plus per-flow middlewares, loaded as `.so` files
- Builtin middlewares: `auth`, `cors`, `compressor`, `logger`, `recoverer`
- Builtin plugins: `camelify`, `snakeify`, `masker`
- Skeleton generator: `aastroctl plugin init`

**Operations**
- Single YAML file, validated ahead of time by the same loader the gateway uses
- OpenAPI 3.1/3.0 export and import via `aastroctl`
- Multi-arch (amd64/arm64) distroless-style image on `chainguard/wolfi-base`, running as a non-root user

---

## `aastroctl`: OpenAPI in both directions

The gateway config is the source of truth, and `aastroctl` turns it into a spec your clients
can consume - or turns someone else's spec into a starting config.

```bash
# Generate an OpenAPI 3.1 document from the gateway configuration.
# Statuses are derived from the actual config: 206 only for best-effort fan-out,
# 429 only when the rate limiter is on, 401 only behind the auth middleware.
aastroctl openapi export -c aastro.yaml -o openapi.yaml

# Round-trippable export: embeds x-aastro snapshots (never secrets).
aastroctl openapi export -c aastro.yaml --extensions -o openapi.yaml

# Scaffold a gateway configuration from any OpenAPI 3.x document.
aastroctl openapi import -i openapi.yaml -o aastro.yaml --default-host https://backend.internal
```

Export output is deterministic and diff-friendly, so the generated spec can live in git and be
checked in CI. Import validates its own output - it never emits a config the gateway would
reject.

---

## Zero-downtime TLS certificate rotation

Aastro reloads TLS material - on both the inbound data port and outbound upstream connections -
without restarting the process, reloading the config, or dropping connections. It watches the
certificate *directories* and atomically swaps the in-memory material when files change.

- **Hands-off with your cert manager.** Directory-level watching covers both atomic replacement
  on a host (write-temp-then-rename) and Kubernetes secret mounts, where projected files rotate
  via symlink swap.
- **Safe by construction.** New handshakes use the new certificate; in-flight connections finish
  on the old one. If a rotated certificate or CA bundle fails to parse, the previously loaded
  material stays live - a bad rotation cannot take the listener down.
- **No configuration required.** Rotation works on your existing `cert_file`, `key_file`, and
  `ca_file` paths. There is no flag to enable.

```yaml
gateway:
  server:
    tls:
      enabled: true
      cert_file: /etc/aastro/server.crt   # rotate this file → picked up automatically
      key_file: /etc/aastro/server.key
      client_auth: require
      client_ca_file: /etc/aastro/client-ca.crt

  routing:
    flows:
      - upstreams:
          - tls:
              enabled: true
              cert_file: /etc/aastro/clients/users.crt   # outbound mTLS, also hot-reloaded
              key_file: /etc/aastro/clients/users.key
              ca_file: /etc/aastro/internal-ca.crt
```

---

## Plugins

A plugin is an ordinary Go package built with `-buildmode=plugin` that exports a `NewPlugin`
(or `NewMiddleware`) factory:

```bash
aastroctl plugin init --type response --name tenant_masker --author you
CGO_ENABLED=1 go build -buildmode=plugin -trimpath -o /etc/aastro/plugins/tenant_masker.so ./tenant_masker
```

```yaml
plugins:
  - name: tenant_masker
    source: file            # builtin | file
    path: /etc/aastro/plugins/    # directory; <name>.so is resolved inside it
    config:
      header: X-Tenant-Id
```

Plugins must be compiled with the exact Go version and dependency set used for the gateway
binary - Go's plugin ABI is unforgiving. See the
[plugin guide](https://starwalkn.github.io/aastro-docs/) and
[CONTRIBUTING.md](.github/CONTRIBUTING.md).

---

## Roadmap

Development is driven by demonstrated demand rather than a fixed feature list. Open a
[discussion](https://github.com/starwalkn/aastro/discussions) or upvote an existing issue -
that is genuinely how the next milestone gets picked.

---

## Contributing

Bug reports, plugins, benchmarks, and documentation fixes are all welcome. Start with
[CONTRIBUTING.md](.github/CONTRIBUTING.md).

## License

Apache-2.0 - see [LICENSE](LICENSE).

---

Made with ❤️ in Go