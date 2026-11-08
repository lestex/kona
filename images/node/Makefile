# Build the kona node image with Apple `container` (no Docker).
include ../../versions.env

REPO  ?= ghcr.io/lestex/kona-node
# OCI tags cannot contain '+': v1.34.11+k3s1 -> v1.34.11-k3s1
TAG   := $(subst +,-,$(K3S_VERSION))
IMAGE := $(REPO):$(TAG)
KUBEADM_IMAGE := $(REPO):$(KUBERNETES_VERSION)-kubeadm
DNS   ?= $(shell scutil --dns | awk '/nameserver\[0\]/{print $$3; exit}')

.PHONY: build build-kubeadm print-image

build:
	container build --dns $(DNS) --cpus 4 --memory 4g \
	  --build-arg FEDORA_DIGEST=$(FEDORA_DIGEST) \
	  --build-arg K3S_VERSION=$(K3S_VERSION) \
	  --build-arg K3S_SHA256=$(K3S_SHA256_ARM64) \
	  --build-arg K3S_AIRGAP_SHA256=$(K3S_AIRGAP_SHA256_ARM64) \
	  -t $(IMAGE) -f Containerfile .

build-kubeadm:
	container build --dns $(DNS) --cpus 4 --memory 4g \
	  --build-arg FEDORA_DIGEST=$(FEDORA_DIGEST) \
	  --build-arg KUBERNETES_VERSION=$(KUBERNETES_VERSION) \
	  --build-arg KUBEADM_SHA256=$(KUBEADM_SHA256_ARM64) \
	  --build-arg KUBELET_SHA256=$(KUBELET_SHA256_ARM64) \
	  --build-arg KUBECTL_SHA256=$(KUBECTL_SHA256_ARM64) \
	  --build-arg CONTAINERD_VERSION=$(CONTAINERD_VERSION) \
	  --build-arg CONTAINERD_SHA256=$(CONTAINERD_SHA256_ARM64) \
	  --build-arg RUNC_VERSION=$(RUNC_VERSION) \
	  --build-arg RUNC_SHA256=$(RUNC_SHA256_ARM64) \
	  --build-arg CNI_PLUGINS_VERSION=$(CNI_PLUGINS_VERSION) \
	  --build-arg CNI_PLUGINS_SHA256=$(CNI_PLUGINS_SHA256_ARM64) \
	  --build-arg CRICTL_VERSION=$(CRICTL_VERSION) \
	  --build-arg CRICTL_SHA256=$(CRICTL_SHA256_ARM64) \
	  --build-arg ETCD_VERSION=$(ETCD_VERSION) \
	  --build-arg ETCD_SHA256=$(ETCD_SHA256_ARM64) \
	  -t $(KUBEADM_IMAGE) -f Containerfile.kubeadm .

print-image:
	@echo $(IMAGE)
