GOOS ?= linux
GOARCH ?= amd64
PLUGIN_OUT=build/plugins
MIDDLEWARE_OUT=build/middlewares

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build plugins clean lint test

all: clean build plugins

build:
	mkdir -p .bin
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 go build -ldflags="$(LDFLAGS)" -trimpath -o .bin/aastro ./cmd/aastro

plugins:
	mkdir -p $(PLUGIN_OUT)
	mkdir -p $(MIDDLEWARE_OUT)

	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -buildmode=plugin -o $(MIDDLEWARE_OUT)/auth.so ./builtin/middlewares/auth
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -buildmode=plugin -o $(MIDDLEWARE_OUT)/compressor.so ./builtin/middlewares/compressor
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -buildmode=plugin -o $(MIDDLEWARE_OUT)/cors.so ./builtin/middlewares/cors
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -buildmode=plugin -o $(MIDDLEWARE_OUT)/logger.so ./builtin/middlewares/logger
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -buildmode=plugin -o $(MIDDLEWARE_OUT)/recoverer.so ./builtin/middlewares/recoverer

	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -buildmode=plugin -o $(PLUGIN_OUT)/camelify.so ./builtin/plugins/camelify
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -buildmode=plugin -o $(PLUGIN_OUT)/masker.so ./builtin/plugins/masker
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -buildmode=plugin -o $(PLUGIN_OUT)/snakeify.so ./builtin/plugins/snakeify

clean:
	rm -rf build/middlewares build/plugins .bin

lint:
	golangci-lint run

test:
	ginkgo -r -p
