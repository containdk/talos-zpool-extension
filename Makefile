# Makefile for Talos ZFS Pool Extension

REGISTRY ?= ghcr.io/containdk
IMAGE_NAME ?= talos-zpool-extension
PLATFORMS ?= linux/amd64,linux/arm64

# Default Talos version to build against. Can be overridden.
TALOS_VERSION ?= v1.12.1
# Get the latest git tag without the 'v' prefix for the application version.
VERSION ?= $(shell git describe --tags --abbrev=0 | sed 's/^v//')
# The full version string used for the manifest and image tag.
FULL_VERSION = $(VERSION)-$(TALOS_VERSION)

IMAGE_URL = $(REGISTRY)/$(IMAGE_NAME)

.PHONY: all build push clean

all: build

# Build for the local host platform and load into the local Docker daemon
build:
	@echo "Building extension image for local platform: $(IMAGE_URL):$(FULL_VERSION)"
	docker buildx build --load \
		--build-arg VERSION=$(VERSION) \
		--build-arg TALOS_VERSION=$(TALOS_VERSION) \
		-t $(IMAGE_URL):$(FULL_VERSION) \
		-t $(IMAGE_URL):latest \
		.

# Build and push the multi-platform manifest for both amd64 and arm64
push:
	@echo "Building and pushing extension image for $(PLATFORMS) as $(IMAGE_URL):$(FULL_VERSION)"
	docker buildx build --platform $(PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		--build-arg TALOS_VERSION=$(TALOS_VERSION) \
		-t $(IMAGE_URL):$(FULL_VERSION) \
		-t $(IMAGE_URL):latest \
		--push .

clean:
	@echo "Removing local images..."
	@docker rmi $(IMAGE_URL):$(FULL_VERSION) >/dev/null 2>&1 || true
	@docker rmi $(IMAGE_URL):latest >/dev/null 2>&1 || true
	@echo "Clean complete."
