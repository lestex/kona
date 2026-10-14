# Build the kona node image with Apple `container` (no Docker).
include ../../versions.env

REPO  ?= ghcr.io/lestex/kona-node
# OCI tags cannot contain '+': v1.34.11+k3s1 -> v1.34.11-k3s1
TAG   := $(subst +,-,$(K3S_VERSION))
IMAGE := $(REPO):$(TAG)
DNS   ?= $(shell scutil --dns | awk '/nameserver\[0\]/{print $$3; exit}')

.PHONY: build print-image

build:
	container build --dns $(DNS) --cpus 4 --memory 4g \
	  --build-arg FEDORA_DIGEST=$(FEDORA_DIGEST) \
	  --build-arg K3S_VERSION=$(K3S_VERSION) \
	  --build-arg K3S_SHA256=$(K3S_SHA256_ARM64) \
	  --build-arg K3S_AIRGAP_SHA256=$(K3S_AIRGAP_SHA256_ARM64) \
	  -t $(IMAGE) -f Containerfile .

print-image:
	@echo $(IMAGE)
