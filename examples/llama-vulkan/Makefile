include ../../versions.env

VULKAN_IMAGE ?= ghcr.io/lestex/kona-vulkan:$(MESA_VERSION)
IMAGE        ?= ghcr.io/lestex/kona-llama-vulkan:$(LLAMA_CPP_VERSION)
CLUSTER      ?= kona
KONA         ?= ../../bin/kona
DNS          ?= $(shell scutil --dns | awk '/nameserver\[0\]/{print $$3; exit}')
TAR          := /tmp/kona-$(shell id -u)/llama-vulkan.tar

.PHONY: image load

image:
	container build --dns $(DNS) --cpus 6 --memory 6g \
	  --build-arg VULKAN_IMAGE=$(VULKAN_IMAGE) \
	  --build-arg LLAMA_CPP_VERSION=$(LLAMA_CPP_VERSION) --build-arg LLAMA_CPP_SHA256=$(LLAMA_CPP_SHA256) \
	  --build-arg LLAMA_MODEL_URL=$(LLAMA_MODEL_URL) --build-arg LLAMA_MODEL_SHA256=$(LLAMA_MODEL_SHA256) \
	  -t $(IMAGE) -f Containerfile .

# Until the kona-registry (Phase 4): copy the image into each GPU node.
load:
	@mkdir -p $(dir $(TAR))
	container image save --platform linux/arm64 -o $(TAR) $(IMAGE)
	@for n in $$($(KONA) get nodes --name $(CLUSTER) -o json | python3 -c 'import sys,json;[print(n["name"]) for n in json.load(sys.stdin) if n["kind"]=="krunkit"]'); do \
	  echo "loading $(IMAGE) into $$n"; \
	  $(KONA) ssh --name $(CLUSTER) $$n -- sudo k3s ctr -n k8s.io images import --platform linux/arm64 - < $(TAR) || exit 1; \
	done
	@rm -f $(TAR)
