# Talos ZFS Pool Extension

This repository contains a Talos system extension service designed to create a ZFS zpool automatically on boot.

## How it Works

This project is implemented as a Talos [System Extension](https://docs.siderolabs.com/talos/v1.8/build-and-extend-talos/custom-images-and-development/system-extensions/). It uses a Go-based binary to perform the ZFS pool creation logic, which is necessary as Talos provides a minimal environment without a shell.

The extension provides:
1.  **A Go Binary**: Located at `/usr/local/lib/containers/zpool-creator/create-zpool`.
2.  **A Service Definition**: Located at `/usr/local/etc/containers/zpool-creator.yaml`.

The service is configured to depend on the `zfs` extension and `configuration` availability. It is idempotent; if the pool already exists, the service exits successfully without doing anything.

## Usage

### Prerequisites

This extension requires the standard Sidero Labs `zfs` extension to be installed on the Talos node, as it relies on the `zpool` binary provided by that extension.

### Building the Extension

Use the provided `Makefile` to build the extension image. The build process uses a multi-stage Dockerfile to compile the Go binary and package it into a minimal `scratch` image.

```sh
# Build the extension image
make build

# Push to your container registry
make push
```

### Talos Configuration

#### 1. Add the Extension

System extensions should be included at image creation time using the Talos `imager` tool. Use the `--system-extension-image` flag to include this extension and the required ZFS extension.

```sh
docker run -t --rm -v .:/work --privileged ghcr.io/siderolabs/imager:v1.12.1 \
  iso \
  --system-extension-image ghcr.io/siderolabs/zfs:2.4.0-v1.12.1 \
  --system-extension-image ghcr.io/containdk/talos-zpool-extension:latest
```

#### 2. Configure the Service

Once the node is running with the extension, configure it by applying an `ExtensionServiceConfig` document. This is where you specify which disks to use for the pool.

```yaml
apiVersion: v1alpha1
kind: ExtensionServiceConfig
name: zpool-creator
environment:
  - ZPOOL_NAME=csi
  - ZPOOL_DISKS=/dev/sdb /dev/sdc
  - ZPOOL_TYPE=mirror
  - ASHIFT=12
```

### Configuration Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `ZPOOL_NAME` | `tank` | The name of the ZFS pool to create. |
| `ZPOOL_DISKS` | (Required) | A space-separated list of block devices (e.g., `/dev/sdb /dev/sdc`). |
| `ZPOOL_TYPE` | (Optional) | The vdev type (e.g., `mirror`, `raidz`, `raidz2`). If empty, disks are added as individual vdevs. |
| `ASHIFT` | `12` | The ashift value for the pool. |

## Development

The creator is written in Go to ensure compatibility with the Talos environment. 
The source code and its Go module files are located in the `create-zpool/` directory.

- `create-zpool/main.go`: The source code for the creator binary.
- `zpool-creator.yaml`: The Talos service definition.
- `Dockerfile`: The multi-stage build definition.
