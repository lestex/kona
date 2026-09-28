include ../../versions.env

VULKAN_IMAGE ?= ghcr.io/lestex/kona-vulkan:$(MESA_VERSION)
IMAGE        ?= ghcr.io/lestex/kona-venus-dra:$(KONA_VENUS_DRA_VERSION)
DNS          ?= $(shell scutil --dns | awk '/nameserver\[0\]/{print $$3; exit}')

.PHONY: test images vulkan-image image

test:
	go test ./...

images: vulkan-image image

vulkan-image:
	container build --dns $(DNS) --cpus 4 --memory 4g \
	  --build-arg FEDORA_DIGEST=$(FEDORA_DIGEST) --build-arg MESA_VERSION=$(MESA_VERSION) \
	  -t $(VULKAN_IMAGE) -f ../../images/vulkan/Containerfile ../../images/vulkan

image:
	container build --dns $(DNS) --cpus 4 --memory 4g \
	  --build-arg GOLANG_DIGEST=$(GOLANG_DIGEST) --build-arg VULKAN_IMAGE=$(VULKAN_IMAGE) \
	  -t $(IMAGE) -f Containerfile .
