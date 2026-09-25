VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
MATRIX  := $(shell grep -E '^[A-Z0-9_]+=' versions.env | paste -sd, -)
PKG     := github.com/lestex/kona/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X '$(PKG).matrix=$(MATRIX)'

.PHONY: build test lint clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/kona ./cmd/kona

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -rf bin dist
