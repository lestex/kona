# Build the kona guest kernel for Apple container, entirely inside a
# `container` VM (no Docker). Output: kernel/out/vmlinux-<ver>-kona
include ../versions.env

IMAGE   := kona-kernel-builder:$(KERNEL_VERSION)
TARBALL := out/linux-$(KERNEL_VERSION).tar.xz
MAJOR   := $(firstword $(subst ., ,$(KERNEL_VERSION)))
# The vmnet gateway DNS forwarder is not reliable on every host (see
# docs/failure-modes.md), so pass the host's primary resolver explicitly.
DNS     ?= $(shell scutil --dns | awk '/nameserver\[0\]/{print $$3; exit}')

.PHONY: all image kernel clean

all: kernel

image:
	container build --dns $(DNS) --cpus 4 --memory 4g -t $(IMAGE) -f Containerfile .

$(TARBALL):
	mkdir -p out
	curl -fsSL -o $@ https://cdn.kernel.org/pub/linux/kernel/v$(MAJOR).x/linux-$(KERNEL_VERSION).tar.xz
	curl -fsSL https://cdn.kernel.org/pub/linux/kernel/v$(MAJOR).x/sha256sums.asc \
	  | grep " linux-$(KERNEL_VERSION).tar.xz" | sed 's|linux-|out/linux-|' | shasum -a 256 -c -

kernel: image $(TARBALL)
	container run --rm --cpus 8 --memory 8g --dns $(DNS) \
	  -v $(CURDIR):/kernel -w /kernel \
	  -e KERNEL_VERSION=$(KERNEL_VERSION) \
	  $(IMAGE) /bin/bash /kernel/build.sh

clean:
	rm -rf out
