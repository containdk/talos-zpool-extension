# Makefile for Talos ZFS Pool Extension

REGISTRY ?= ghcr.io/containdk
IMAGE_NAME ?= talos-zpool-extension
TAG ?= $(shell git rev-parse --short HEAD || echo "latest")
IMAGE_URL = $(REGISTRY)/$(IMAGE_NAME)

.PHONY: all build push clean

all: build

build:
	@echo "Building extension image: $(IMAGE_URL):$(TAG)"
	@docker build -t $(IMAGE_URL):$(TAG) .
	@docker tag $(IMAGE_URL):$(TAG) $(IMAGE_URL):latest

push:
	@echo "Pushing extension image: $(IMAGE_URL):$(TAG) and :latest"
	@docker push $(IMAGE_URL):$(TAG)
	@docker push $(IMAGE_URL):latest

clean:
	@echo "Removing local images..."
	@docker rmi $(IMAGE_URL):$(TAG) >/dev/null 2>&1 || true
	@docker rmi $(IMAGE_URL):latest >/dev/null 2>&1 || true
	@echo "Clean complete."
