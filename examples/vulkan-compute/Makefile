include ../../versions.env

VULKAN_IMAGE ?= ghcr.io/lestex/kona-vulkan:$(MESA_VERSION)
IMAGE        ?= ghcr.io/lestex/kona-vulkan-compute:v0.1.0
CLUSTER      ?= kona
KONA         ?= ../../bin/kona
DNS          ?= $(shell scutil --dns | awk '/nameserver\[0\]/{print $$3; exit}')
TAR          := /tmp/kona-$(shell id -u)/vkcompute.tar

.PHONY: image load

image:
	container build --dns $(DNS) --cpus 4 --memory 4g --build-arg VULKAN_IMAGE=$(VULKAN_IMAGE) -t $(IMAGE) -f Containerfile .

# Until the kona-registry (Phase 4): copy the image into each GPU node.
load:
	@mkdir -p $(dir $(TAR))
	container image save --platform linux/arm64 -o $(TAR) $(IMAGE)
	@for n in $$($(KONA) get nodes --name $(CLUSTER) -o json | python3 -c 'import sys,json;[print(n["name"]) for n in json.load(sys.stdin) if n["kind"]=="krunkit"]'); do \
	  echo "loading $(IMAGE) into $$n"; \
	  $(KONA) ssh --name $(CLUSTER) $$n -- sudo k3s ctr -n k8s.io images import --platform linux/arm64 - < $(TAR) || exit 1; \
	done
	@rm -f $(TAR)
