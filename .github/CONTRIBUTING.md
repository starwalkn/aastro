# Contributing to Aastro

Thank you for your interest in Aastro - a Go-based API Gateway with support for dynamic `.so` plugins that can freely modify the original `*http.Request` and the aggregated `*http.Response`.

We welcome all kinds of contributions: bug fixes, performance improvements, documentation updates, tests, architectural improvements, and new features.

---

## 📦 Requirements

- Go (version compatible with `go.mod`)
- Docker (recommended for running and testing)
- GNU toolchain (required for building `.so` plugins using `-buildmode=plugin`)

> ⚠️ Important: Go plugins require the exact same Go version used to build the gateway binary.

---

## 🚀 Quick Start (Local Development)

```bash
git clone https://github.com/starwalkn/aastro.git
cd aastro

make all GOOS=<YOUR_OS> GOARCH=<YOUR_ARCH>

./bin/aastro serve
```

CLI commands:

```bash
aastro serve
aastro validate
```

---

## 🐳 Running with Docker (Recommended)

```bash
docker build -f build/Dockerfile -t aastro:local .
docker run -p 8705:8705 -v $(pwd)/<your_config>.yaml:/app/aastro.yaml -e AASTRO_CONFIG=/app/aastro.yaml aastro:local
```

If using docker-compose:

```bash
docker compose up --build
```

---

## 🔌 Plugin Development

Aastro supports Go plugins compiled as `.so` files:

```bash
CGO_ENABLED=1 go build -buildmode=plugin -o myplugin.so ./plugins/myplugin
```

### Plugin Requirements

- Must expose the expected exported symbol (document the required interface here if applicable)
- A plugin may:
  - Modify `*http.Request`
  - Modify or wrap the aggregated `*http.Response`
  - Inject or modify headers
  - Perform logging
  - Validate requests
  - Short-circuit responses

### Important Notes

- Plugins must be compiled with the same Go version as the gateway.
- ABI mismatches will result in runtime failures.
- Avoid unsafe operations unless absolutely necessary.
- Be careful with shared state - plugins run inside the gateway process.

---

## 🌿 Development Workflow

1. Fork the repository
2. Create a branch from `main`

Branch naming convention:

```
feature/<short-description>
fix/<short-description>
refactor/<short-description>
```

3. Make your changes
4. Add or update tests
5. Ensure everything builds
6. Open a Pull Request

---

## 🧪 Testing

Run tests with:

```bash
make test
```

If your changes affect:

- Plugin loading
- Request/response mutation
- Proxying logic
- Aggregation pipeline

Please add integration tests where appropriate.

---

## 🧹 Code Style

- Run `go fmt`
- Run `go vet`
- Follow idiomatic Go practices
- Avoid unnecessary global state
- Always handle errors explicitly
- Keep functions focused and small when possible

Before submitting a PR, please run:

```bash
make lint
make test
make all
```

---

## 🛡 Pull Request Guidelines

A good PR should:

- Have a clear and descriptive title
- Explain the motivation behind the change
- Avoid unrelated modifications
- Include tests when applicable
- Not introduce breaking changes without discussion

Smaller PRs are reviewed faster.

---

## 🐞 Reporting Bugs

When opening an Issue, please include:

- Go version
- Operating system
- How you are running Aastro (Docker or local)
- Gateway configuration
- Example request (if relevant)
- Expected behavior
- Actual behavior
- Logs (if available)

Minimal reproducible examples are highly appreciated.

---

## 💡 Feature Requests

Before implementing a major feature:

1. Open an Issue describing the use case
2. Explain how it fits into the gateway architecture
3. Propose a possible implementation approach (if available)

This helps avoid unnecessary or conflicting work.

---

## 🔒 Security

If you discover a security vulnerability (especially related to:

- Plugin execution
- Request mutation
- SSRF
- Header injection
- Remote code execution

Please do NOT open a public issue.

Contact the maintainers privately.

---

## 🤝 Ways to Contribute

You can help by:

- Improving documentation
- Writing example plugins
- Adding benchmarks
- Expanding test coverage
- Improving CLI developer experience
- Refactoring architecture
- Reviewing open PRs

---

Thank you for contributing to Aastro 🚀