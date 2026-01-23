# Use a specific version of Go for stability
FROM --platform=$BUILDPLATFORM golang:1.21-alpine AS builder

# Set the working directory
WORKDIR /src

# Leverage Docker cache by copying dependency files first (if any existed, e.g., go.mod)
# For now, we only have main.go
COPY main.go .

# Arguments provided by Docker Buildx for cross-compilation
ARG TARGETOS
ARG TARGETARCH

# Build the Go binary for the target architecture
# CGO_ENABLED=0 ensures a static binary which is required for the scratch image
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 \
    go build -ldflags="-s -w" -o create-zpool main.go

# Final stage: minimal image
FROM scratch

# Copy the extension manifest and service definition
COPY manifest.yaml /manifest.yaml
COPY zpool-creator.yaml /rootfs/usr/local/etc/containers/zpool-creator.yaml

# Copy the compiled binary from the builder stage
COPY --from=builder /src/create-zpool /rootfs/usr/local/lib/containers/zpool-creator/create-zpool
