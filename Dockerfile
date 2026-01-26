# Use a specific version of Go for stability
FROM --platform=$BUILDPLATFORM golang:1.25-alpine@sha256:d9b2e14101f27ec8d09674cd01186798d227bb0daec90e032aeb1cd22ac0f029 AS builder

# Set the working directory
WORKDIR /src

# Arguments for versioning, passed from Makefile
ARG VERSION
ARG TALOS_VERSION

# Generate the manifest file
RUN cat > /src/manifest.yaml <<EOF
version: v1alpha1
metadata:
  name: zpool-creator
  version: "${VERSION}-${TALOS_VERSION}"
  author: KimNorgaard
  description: |
    [extra] This system extension provides a service to create zpools on boot.
  compatibility:
    talos:
      version: ">= v1.12.0"
EOF

# Leverage Docker cache by copying dependency files first
COPY create-zpool/ .

# Arguments provided by Docker Buildx for cross-compilation
ARG TARGETOS
ARG TARGETARCH

# Build the Go binary for the target architecture
# CGO_ENABLED=0 ensures a static binary which is required for the scratch image
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 \
    go build -ldflags="-s -w" -o create-zpool .

# Final stage: minimal image
FROM scratch

# Copy the generated manifest from the builder stage
COPY --from=builder /src/manifest.yaml /manifest.yaml
# Copy the extension service definition
COPY zpool-creator.yaml /rootfs/usr/local/etc/containers/zpool-creator.yaml

# Copy the compiled binary from the builder stage
COPY --from=builder /src/create-zpool /rootfs/usr/local/lib/containers/zpool-creator/create-zpool
