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

Once the node is running with the extension, configure it by applying an `ExtensionServiceConfig` document. This is where you specify the pools to create using indexed environment variables.

The extension will look for `ZPOOL_NAME_0`, `ZPOOL_NAME_1`, and so on, creating a pool for each index it finds. If one pool fails, the extension will log the error and continue to the next. It will exit with an error only after attempting all configurations.

**Example: Create two pools**
```yaml
apiVersion: v1alpha1
kind: ExtensionServiceConfig
name: zpool-creator
environment:
  # First pool: a mirrored tank
  - ZPOOL_NAME_0=tank
  - ZPOOL_DISKS_0=/dev/sdb /dev/sdc
  - ZPOOL_TYPE_0=mirror
  - ASHIFT_0=12

  # Second pool: a single-disk pool
  - ZPOOL_NAME_1=data
  - ZPOOL_DISKS_1=/dev/sdd

  # Global ASHIFT fallback (used if ASHIFT_n is not set for a pool)
  - ASHIFT=12
```

### Configuration Variables

The extension is configured by defining one or more pools using indexed environment variables. The process starts at index `0` and continues as long as a `ZPOOL_NAME_<n>` is found.

For each pool `n` (e.g., `0`, `1`, `2`, ...), the following variables are used:

| Variable | Required? | Description |
| :--- | :--- | :--- |
| `ZPOOL_NAME_<n>` | **Yes** | The name of the ZFS pool to create (e.g., `ZPOOL_NAME_0=tank`). |
| `ZPOOL_DISKS_<n>` | **Yes** | A space-separated list of block devices (e.g., `ZPOOL_DISKS_0=/dev/sdb /dev/sdc`). |
| `ZPOOL_TYPE_<n>` | No | The vdev type (`mirror`, `raidz`, etc.). If empty, disks are added as individual vdevs. |
| `ASHIFT_<n>` | No | The `ashift` value for this specific pool. If not set, it falls back to the global `ASHIFT` value. |

A global `ASHIFT` can also be set as a default for all pools.

| Variable | Default | Description |
| :--- | :--- | :--- |
| `ASHIFT` | `12` | The global `ashift` value to use if a pool-specific `ASHIFT_<n>` is not defined. |

## Development

The creator is written in Go to ensure compatibility with the Talos environment. 
The source code and its Go module files are located in the `create-zpool/` directory.

- `create-zpool/main.go`: The source code for the creator binary.
- `zpool-creator.yaml`: The Talos service definition.
- `Dockerfile`: The multi-stage build definition.
