# Makefile for Talos ZFS Pool Extension

REGISTRY ?= ghcr.io/containdk
IMAGE_NAME ?= talos-zpool-extension
TAG ?= $(shell git rev-parse --short HEAD || echo "latest")
IMAGE_URL = $(REGISTRY)/$(IMAGE_NAME)
PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: all build push clean

all: build

# Build for the local host platform and load into the local Docker daemon
build:
	@echo "Building extension image for local platform: $(IMAGE_URL):$(TAG)"
	docker buildx build --load -t $(IMAGE_URL):$(TAG) .
	docker tag $(IMAGE_URL):$(TAG) $(IMAGE_URL):latest

# Build and push the multi-platform manifest for both amd64 and arm64
push:
	@echo "Building and pushing extension image for $(PLATFORMS)"
	docker buildx build --platform $(PLATFORMS) \
		-t $(IMAGE_URL):$(TAG) \
		-t $(IMAGE_URL):latest \
		--push .

clean:
	@echo "Removing local images..."
	@docker rmi $(IMAGE_URL):$(TAG) >/dev/null 2>&1 || true
	@docker rmi $(IMAGE_URL):latest >/dev/null 2>&1 || true
	@echo "Clean complete."
