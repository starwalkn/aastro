GOOS ?= linux
GOARCH ?= amd64
PLUGIN_OUT=build/plugins
MIDDLEWARE_OUT=build/middlewares

ifndef VERSION
	VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
endif

COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

.PHONY: all build aastro aastroctl plugins clean lint test

all: clean build plugins

build: aastro aastroctl

aastro:
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 go build -ldflags="$(LDFLAGS)" -trimpath -o .bin/aastro ./cmd/aastro

aastroctl:
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags="$(LDFLAGS)" -trimpath -o .bin/aastroctl ./cmd/aastroctl

MIDDLEWARES := $(notdir $(wildcard builtin/middlewares/*))
PLUGINS     := $(notdir $(wildcard builtin/plugins/*))

PLUGIN_LDFLAGS := -s -w
PLUGIN_BUILDFLAGS := -trimpath -ldflags="$(PLUGIN_LDFLAGS)" -buildmode=plugin

plugins: $(addprefix $(MIDDLEWARE_OUT)/,$(addsuffix .so,$(MIDDLEWARES))) \
         $(addprefix $(PLUGIN_OUT)/,$(addsuffix .so,$(PLUGINS)))

$(MIDDLEWARE_OUT)/%.so: builtin/middlewares/% | $(MIDDLEWARE_OUT)
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 go build $(PLUGIN_BUILDFLAGS) -o $@ ./$<

$(PLUGIN_OUT)/%.so: builtin/plugins/% | $(PLUGIN_OUT)
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 go build $(PLUGIN_BUILDFLAGS) -o $@ ./$<

$(MIDDLEWARE_OUT) $(PLUGIN_OUT):
	mkdir -p $@

clean:
	rm -rf build/middlewares build/plugins .bin

lint:
	golangci-lint run

test:
	ginkgo -r -p
