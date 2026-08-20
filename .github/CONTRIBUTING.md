# Contributing to Aastro

Thanks for your interest in Aastro - a Go API Gateway with parallel fan-out, declarative
response aggregation, and dynamically loaded `.so` plugins that can modify the incoming
`*http.Request` and the aggregated response.

All kinds of contributions are welcome: bug fixes, performance work, documentation, tests,
example plugins, and features. If you are unsure whether something fits, open a
[discussion](https://github.com/starwalkn/aastro/discussions) before writing code.

---

## Requirements

- **Go** - the version in [`go.mod`](../go.mod). Not "a compatible version": Go's plugin ABI
  requires the *exact* toolchain version for the gateway and every `.so` it loads.
- **A C toolchain** (`gcc`/`clang` + `make`) - the gateway and all plugins are built with
  `CGO_ENABLED=1` and `-buildmode=plugin`.
- **Docker** - optional, but the fastest way to reproduce the release build.
- **Ginkgo CLI** for running the suites locally:
  `go install github.com/onsi/ginkgo/v2/ginkgo@latest`
- **golangci-lint** matching the version pinned in
  [`.github/workflows/checks.yml`](workflows/checks.yml).

> Go plugins only work on Linux, macOS, and FreeBSD. There is no plugin support on Windows -
> use WSL2 or the Docker build there.

---

## Local development

```bash
git clone https://github.com/starwalkn/aastro.git
cd aastro

# Builds .bin/aastro, .bin/aastroctl, and every builtin plugin/middleware
# into build/plugins and build/middlewares.
make all GOOS=<YOUR_OS> GOARCH=<YOUR_ARCH>    # e.g. GOOS=darwin GOARCH=arm64

./.bin/aastro -c sample.config.yaml
```

`GOOS` and `GOARCH` default to `linux/amd64`, so set them explicitly unless that is your host.

### CLI surface

The gateway itself is flag-driven, with no subcommands:

```
aastro [options]

  -c, --config <path>     configuration file path (env: AASTRO_CONFIG,
                          default: /etc/aastro/config.yaml)
  -t, --test              validate the configuration and exit
  -T, --test-dump         validate, print the effective configuration, and exit
  -q, --quiet             suppress non-error output
  -v, --version           print version
  -V, --version-verbose   print version with build details
```

`aastroctl` is the companion tool and *does* use subcommands:

```
aastroctl plugin init --type <request|response|middleware> --name <name>
aastroctl openapi export -c aastro.yaml -o openapi.yaml [--extensions] [--oas-version 3.0]
aastroctl openapi import -i openapi.yaml -o aastro.yaml [--default-host https://...]
```

Configuration is resolved in this order: `-c` flag → `AASTRO_CONFIG` → `/etc/aastro/config.yaml`.

### Running in Docker

```bash
docker build -f build/Dockerfile -t aastro:local .

docker run \
  -p 7805:7805 \
  -v "$(pwd)/sample.config.yaml:/etc/aastro/config.yaml:ro" \
  aastro:local
```

The image bundles the builtin plugins at `/usr/local/lib/aastro/plugins/` and middlewares at
`/usr/local/lib/aastro/middlewares/`, which is where `source: builtin` resolves them. Note that
the admin port binds to `127.0.0.1` by default - set `gateway.admin.bind_addr: 0.0.0.0` if you
want to reach `/metrics` or `/__health` from outside the container.

---

## Testing

```bash
make test          # ginkgo -r -p
ginkgo -r -race    # run this before pushing anything touching concurrency
```

Tests use [Ginkgo](https://github.com/onsi/ginkgo) and Gomega, with `DescribeTable` for
table-driven cases. Match the surrounding style of the package you are changing.

Please add or extend tests when your change touches:

- plugin and middleware loading
- request/response mutation
- the scatter/aggregation pipeline
- retry, circuit breaker, or load-balancing decisions
- TLS setup or certificate reloading

Concurrency-sensitive areas (fan-out, the certificate holder, the circuit breaker) should be
exercised with `-race`; CI will run it too, but a local failure is much cheaper.

---

## Code style

- `gofmt` and `go vet` clean; `make lint` must pass (`golangci-lint run`).
- Comments explain **why**, not what. Skip comments that restate the code.
- Errors are wrapped with context (`fmt.Errorf("load config: %w", err)`) and always handled.
- No package-level mutable state.
- Keep changes scoped: no drive-by refactoring in a bug-fix PR.

Before opening a PR:

```bash
make lint
make test
make all
```

---

## Writing a plugin

Generate a skeleton, then build it as a shared object:

```bash
aastroctl plugin init --type response --name tenant_masker --author you
CGO_ENABLED=1 go build -buildmode=plugin -trimpath -o ./build/plugins/tenant_masker.so ./tenant_masker
```

Contract:

- **Plugins** must export `func NewPlugin() sdk.Plugin` and implement `Info()`, `Init(map[string]interface{}) error`,
  `Type() sdk.PluginType`, and `Execute(sdk.Context) error`.
- **Middlewares** must export `func NewMiddleware() sdk.Middleware` and implement `Name()`,
  `Init(map[string]interface{}) error`, and `Handler(next http.Handler) http.Handler`.
- Implement `sdk.Closer` if the plugin owns resources that need releasing on shutdown.
- `PluginTypeRequest` runs before upstream dispatch; `PluginTypeResponse` runs after aggregation.
  Response-phase plugins do **not** run on streaming flows - the body is already streaming.

Referencing it from a config:

```yaml
plugins:
  - name: tenant_masker
    source: file                  # builtin | file
    path: /etc/aastro/plugins/    # a directory; <name>.so is resolved inside it
    config:
      header: X-Tenant-Id
```

Things that will bite you:

- A Go version or dependency-version mismatch between the gateway and a plugin surfaces as an
  opaque runtime load failure, not a helpful error. Build both from the same checkout.
- Plugins run inside the gateway process and share its memory. Keep them free of global mutable
  state and safe for concurrent use across requests.
- A panic in a plugin takes the request down; keep the `recoverer` middleware in the chain and
  do not rely on it as a design.

---

## Pull requests

1. Fork the repository and branch from **`master`**.
2. Name the branch after the change: `feature/<short-description>`, `fix/<short-description>`,
   `refactor/<short-description>`, `docs/<short-description>`.
3. Write commits in [Conventional Commits](https://www.conventionalcommits.org/) style
   (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`) - the changelog and release notes
   are derived from them.
4. Update [`CHANGELOG.md`](../CHANGELOG.md) under *Unreleased* for anything user-visible, and
   [`sample.config.yaml`](../sample.config.yaml) for any new configuration field.
5. Open the PR with a title that reads well in a release note, and a description explaining the
   motivation and the trade-offs you considered.

Breaking changes to the configuration schema need discussion first, plus a migration note in the
changelog. Small PRs get reviewed faster than large ones.

---

## Reporting bugs

Open an [issue](https://github.com/starwalkn/aastro/issues/new/choose) and include:

- Aastro version (`aastro -V`), Go version, and OS
- how you run it: Docker image, or a local build
- the smallest configuration that reproduces the problem (redact secrets and hosts)
- the request you sent and the response you got, including status code and the `errors` array
- relevant logs - `debug: true` in the config makes the router, scatter, and upstream layers verbose

A minimal reproducible example is worth more than a long description.

## Feature requests

Aastro ships lean on purpose: features land when there is demonstrated demand, not because a
competitor has them. Before building something large, open an issue or discussion describing the
use case you are stuck on, how it fits the current architecture, and - if you have one - a
sketch of the implementation. That conversation is cheap; a rejected 2,000-line PR is not.

---

## Other ways to help

Documentation fixes, example plugins, benchmarks, coverage for thin spots, and reviews of open
PRs are all genuinely useful, and all of them are easier entry points than the core proxy path.

Thank you for contributing to Aastro 🚀