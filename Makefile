VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
MATRIX  := $(shell grep -E '^[A-Z0-9_]+=' versions.env | grep -vE '_(SHA256[A-Z0-9_]*|DIGEST)=' | paste -sd, -)
SUMS    := $(shell grep -E '^[A-Z0-9_]+_SHA256[A-Z0-9_]*=' versions.env | paste -sd, -)
PKG     := github.com/lestex/kona/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X '$(PKG).matrix=$(MATRIX)' -X '$(PKG).checksums=$(SUMS)'

.PHONY: build test lint fmt-check check snapshot changelog release-env clean

# The release build (.goreleaser.yaml) links the same matrix from these.
export KONA_MATRIX := $(MATRIX)
export KONA_SUMS   := $(SUMS)

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/kona ./cmd/kona

test:
	go test ./...

lint:
	go vet ./...

fmt-check:
	@unformatted=$$(git ls-files '*.go' | xargs gofmt -l); \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

# Everything a commit, and a release tag, must pass.
check: fmt-check lint test

# The release build, for this Mac only, into dist/.
snapshot:
	goreleaser build --snapshot --clean --single-target

# CHANGELOG.md from the commit log, by the same cliff.toml the release
# notes come from. git-cliff from PATH (brew install git-cliff), else its
# pinned image through `container`; keep the pin in step with release.yml.
# TAG files the pending commits under the release being prepared:
#   make changelog TAG=v0.1.0
CLIFF_IMAGE := docker.io/orhunp/git-cliff:2.13.1
CLIFF_ARGS  := --config cliff.toml $(if $(TAG),--tag $(TAG))

changelog:
	@if command -v git-cliff >/dev/null 2>&1; then \
		git-cliff $(CLIFF_ARGS) --output CHANGELOG.md; \
	else \
		echo "git-cliff not on PATH; using $(CLIFF_IMAGE)"; \
		container run --rm -v "$(CURDIR)":/app -w /app $(CLIFF_IMAGE) \
			$(CLIFF_ARGS) > CHANGELOG.md.tmp && mv CHANGELOG.md.tmp CHANGELOG.md \
			|| { rm -f CHANGELOG.md.tmp; exit 1; }; \
	fi
	@echo "wrote CHANGELOG.md"

# For CI: the release build's environment, as KEY=VALUE lines.
release-env:
	@echo "KONA_MATRIX=$(MATRIX)"
	@echo "KONA_SUMS=$(SUMS)"

clean:
	rm -rf bin dist
