<h1 align="center">Aastro API Gateway</h1>

<p align="center">
A lightweight, modular, and high-performance <strong>API Gateway</strong> for modern microservices.
</p>

<p align="center">
Built with simplicity, performance, and developer-friendly configuration in mind.
</p>

[![Go Version](https://img.shields.io/badge/go-1.26.3-blue)](https://golang.org)
[![License](https://img.shields.io/github/license/starwalkn/aastro)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/starwalkn/aastro)](https://goreportcard.com/report/github.com/starwalkn/aastro)
[![codecov](https://codecov.io/gh/starwalkn/aastro/branch/master/graph/badge.svg)](https://codecov.io/gh/starwalkn/aastro)
![Docker Pulls](https://img.shields.io/docker/pulls/starwalkn/aastro)
![GitHub Created At](https://img.shields.io/github/created-at/starwalkn/aastro)
[![GitHub release](https://img.shields.io/github/v/release/starwalkn/aastro)](https://github.com/starwalkn/aastro/releases)

---

## ✨ Features

- 🚀 High-performance HTTP reverse proxy
- 🔀 Request fan-out & response aggregation (merge, array, namespace)
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

## 📖 Documentation

Full documentation, configuration reference, and plugin guide are available at:

**[starwalkn.github.io/aastrodocs](https://starwalkn.github.io/aastro-docs/)**

---

## 📄 License

Open-source. See `LICENSE` file for details.

---

<p align="center">
Made with ❤️ in Go
</p>
