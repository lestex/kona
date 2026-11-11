# Build the kona guest kernel for Apple container, entirely inside a
# `container` VM (no Docker). Output: kernel/out/vmlinux-<ver>-kona
include ../versions.env

IMAGE   := kona-kernel-builder:$(KERNEL_VERSION)
TARBALL := out/linux-$(KERNEL_VERSION).tar.xz
MAJOR   := $(firstword $(subst ., ,$(KERNEL_VERSION)))
# The vmnet gateway DNS forwarder is not reliable on every host (see
# docs/reference/failure-modes.md), so pass the host's primary resolver explicitly.
DNS     ?= $(shell scutil --dns | awk '/nameserver\[[0-9]+\]/ && $$3 ~ /^[0-9.]+$$/ {print $$3; exit}')

# CI builds with docker on an arm64 Linux runner by overriding these
# (.github/workflows/kernel.yml); the build itself is the same.
BUILD   ?= container build --dns $(DNS) --cpus 4 --memory 4g
RUN     ?= container run --rm --cpus 8 --memory 8g --dns $(DNS)
KERNEL  := out/vmlinux-$(KERNEL_VERSION)-kona

.PHONY: all image kernel check print-tag print-version clean

all: kernel

image:
	$(BUILD) -t $(IMAGE) -f Containerfile .

$(TARBALL):
	mkdir -p out
	curl -fsSL -o $@ https://cdn.kernel.org/pub/linux/kernel/v$(MAJOR).x/linux-$(KERNEL_VERSION).tar.xz
	curl -fsSL https://cdn.kernel.org/pub/linux/kernel/v$(MAJOR).x/sha256sums.asc \
	  | grep " linux-$(KERNEL_VERSION).tar.xz" | sed 's|linux-|out/linux-|' | shasum -a 256 -c -

kernel: image $(TARBALL)
	$(RUN) -v $(CURDIR):/kernel -w /kernel \
	  -e KERNEL_VERSION=$(KERNEL_VERSION) \
	  $(IMAGE) /bin/bash /kernel/build.sh

# The build is reproducible, so a kernel built anywhere must match the pin
# in versions.env. A mismatch means the config or toolchain changed: update
# KONA_KERNEL_SHA256_ARM64 on purpose, or find what drifted.
check:
	@got=$$(shasum -a 256 $(KERNEL) | cut -d' ' -f1); \
	if [ "$$got" != "$(KONA_KERNEL_SHA256_ARM64)" ]; then \
	  echo "$(KERNEL): sha256 $$got, versions.env pins $(KONA_KERNEL_SHA256_ARM64)"; exit 1; \
	fi; echo "$(KERNEL) matches the pin"

# The GitHub release that holds the pinned kernel (internal/kernel.Tag).
print-tag:
	@echo kernel-$(KERNEL_VERSION)-$(shell printf %s $(KONA_KERNEL_SHA256_ARM64) | cut -c1-12)

print-version:
	@echo $(KERNEL_VERSION)

clean:
	rm -rf out
