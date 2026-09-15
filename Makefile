# Makefile for Talos ZFS Pool Extension

REGISTRY ?= ghcr.io/containdk
IMAGE_NAME ?= talos-zpool-extension
PLATFORMS ?= linux/amd64,linux/arm64

# Default Talos version to build against. Can be overridden.
TALOS_VERSION ?= v1.14
# Get the latest git tag without the 'v' prefix for the application version.
VERSION ?= $(shell git describe --tags --abbrev=0 | sed 's/^v//')
# The full version string used for the manifest and image tag.
FULL_VERSION = $(VERSION)-$(TALOS_VERSION)

IMAGE_URL = $(REGISTRY)/$(IMAGE_NAME)

# Multi-platform builds require the docker-container driver; the "docker"
# driver cannot produce multi-arch manifests. If the currently selected buildx
# builder uses the docker driver (typical for a plain local Docker/Colima
# setup), fall back to a dedicated container builder, created on demand.
# Where a container builder is already selected (e.g. CI), this resolves to
# empty and the selected builder is used as-is.
FALLBACK_BUILDER ?= multiarch
BUILDER = $(shell docker buildx inspect 2>/dev/null | awk -F': *' '/^Driver:/{print $$2}' | grep -qx docker && echo $(FALLBACK_BUILDER))
BUILDER_FLAG = $(if $(BUILDER),--builder $(BUILDER))

.PHONY: all build push clean test check-git-clean check-release-tag buildx-builder

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

# Ensure a multi-platform capable builder exists
buildx-builder:
	@builder='$(BUILDER)'; \
	if [ -n "$$builder" ] && ! docker buildx inspect "$$builder" >/dev/null 2>&1; then \
		echo "Creating buildx builder '$$builder' (docker-container driver)..."; \
		docker buildx create --name "$$builder" --driver docker-container --bootstrap >/dev/null; \
	fi

# Build and push the multi-platform manifest for both amd64 and arm64
push: test check-git-clean check-release-tag buildx-builder
	@echo "Building and pushing extension image for $(PLATFORMS) as $(IMAGE_URL):$(FULL_VERSION)"
	docker buildx build $(BUILDER_FLAG) --platform $(PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		--build-arg TALOS_VERSION=$(TALOS_VERSION) \
		-t $(IMAGE_URL):$(FULL_VERSION) \
		-t $(IMAGE_URL):latest \
		--push .

# =================
# = Quality Gates
# =================
test:
	@echo "--> Running Go static analysis and tests..."
	@echo "    Checking formatting..."
	@if [ -n "$(shell cd create-zpool && go fmt ./...)" ]; then \
		echo "Go formatting issues found. Please run 'go fmt ./create-zpool/...'"; \
		exit 1; \
	fi
	@echo "    Running go vet..."
	(cd create-zpool && go vet .)
	@echo "    Running tests..."
	(cd create-zpool && go test -v -race -coverprofile=coverage.out .)

check-git-clean:
	@if ! git diff-index --quiet HEAD --; then \
		echo "Git working directory is dirty. Please commit or stash changes before building."; \
		exit 1; \
	fi

check-release-tag:
	@if ! git describe --exact-match --tags HEAD > /dev/null 2>&1; then \
		echo "HEAD is not tagged. Please create a new git tag for a release before pushing."; \
		exit 1; \
	fi

clean:
	@echo "Removing local images..."
	@docker rmi $(IMAGE_URL):$(FULL_VERSION) >/dev/null 2>&1 || true
	@docker rmi $(IMAGE_URL):latest >/dev/null 2>&1 || true
	@echo "Clean complete."
