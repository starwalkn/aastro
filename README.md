<h1 align="center">Aastro API Gateway</h1>

<p align="center">
A lightweight, modular, and high-performance <strong>API Gateway</strong> for modern microservices.
</p>

<p align="center">
Built with simplicity, performance, and developer-friendly configuration in mind.
</p>

[![Go Version](https://img.shields.io/badge/go-1.26.3-blue)](https://golang.org)
[![License](https://img.shields.io/github/license/starwalkn/aastro)](LICENSE)
![Docker Pulls](https://img.shields.io/docker/pulls/starwalkn/aastro)
![GitHub Created At](https://img.shields.io/github/created-at/starwalkn/aastro)
[![GitHub release](https://img.shields.io/github/v/release/starwalkn/aastro)](https://github.com/starwalkn/aastro/releases)
[![Coverage Status](https://coveralls.io/repos/github/starwalkn/aastro/badge.svg)](https://coveralls.io/github/starwalkn/aastro)

---

## ✨ Features

- 🚀 High-performance HTTP reverse proxy
- 🔀 Request fan-out & response aggregation (merge, array, namespace)
- 🔐 TLS & mutual TLS (mTLS) — on the inbound data port and per-upstream 
- 🔄 Zero-downtime TLS certificate hot-reload — cert-manager / Vault / SPIFFE ready
- 🧩 Dynamic `.so` plugin system (request & response phase)
- 🔗 Path parameter extraction and forwarding
- 🔁 Retry, circuit breaker & load balancing (round-robin, least-conns)
- 📊 Prometheus metrics with circuit breaker state tracking
- 🛡 Rate limiting & trusted proxy support
- 📦 YAML-based configuration
- 🐳 Docker-ready

---

## 🚀 Quick Start

```bash
git clone https://github.com/starwalkn/aastro.git
cd aastro

make all GOOS=<YOUR_OS> GOARCH=<YOUR_ARCH>
./bin/aastro -c path/to/config.yaml
```

Or with Docker:

```bash
docker run \
  -p 7805:7805 \
  -v $(pwd)/config.yaml:/etc/aastro/config.yaml \
  -e AASTRO_CONFIG=/etc/aastro/config.yaml \
  starwalkn/aastro:latest
```

---

## 🔄 Zero-downtime TLS certificate rotation

Aastro reloads TLS certificates — on both the inbound data port and outbound upstream
connections — without restarting the process, reloading config, or dropping connections.
It watches the certificate directories and atomically swaps the in-memory material when the
files change on disk. No SIGHUP, no full-config reload, no downtime.

Hands-off with your cert manager. Works out of the box with cert-manager, Vault Agent,
and SPIFFE/SPIRE. Directory-level watching handles both atomic file replacement on a host
(write-temp-then-rename) and Kubernetes secret mounts, where projected files rotate via
symlink swap rather than in-place writes.
Safe by construction. New handshakes use the new certificate; in-flight connections
finish on the old one. If a rotated certificate or CA bundle fails to parse, the previously
loaded material stays live — a bad rotation can't take the listener down.
No configuration required. Rotation works on your existing cert_file, key_file, and
ca_file paths — there is no flag to enable.

```yaml
gateway:
  server:
    tls:
      enabled: true
      cert_file: /etc/aastro/server.crt   # rotate this file → picked up automatically
      key_file:  /etc/aastro/server.key
      client_auth: require
      client_ca_file: /etc/aastro/client-ca.crt

  routing:
    flows:
      - upstreams:
        - tls:
            enabled: true
            cert_file: /etc/aastro/clients/users.crt   # outbound mTLS, also hot-reloaded
            key_file:  /etc/aastro/clients/users.key
            ca_file:   /etc/aastro/internal-ca.crt
```

---

## 📖 Documentation

Full documentation, configuration reference, and plugin guide are available at:

**[starwalkn.github.io/aastro-docs](https://starwalkn.github.io/aastro-docs/)**

---

## 📄 License

Open-source. See `LICENSE` file for details.

---

<p align="center">
Made with ❤️ in Go
</p>
