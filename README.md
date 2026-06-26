# Talos ZFS Pool Extension

This repository contains a Talos system extension service designed to create
ZFS zpools automatically on boot or when an ExtensionServiceConfig is provided.

## How it Works

This project is implemented as a Talos [System
Extension](https://docs.siderolabs.com/talos/latest/build-and-extend-talos/custom-images-and-development/system-extensions/).
It uses a Go-based binary to perform the ZFS pool creation logic, which is
handy because Talos provides a minimal environment without a shell.

The extension provides:

1. **A Go Binary**: Located at `/usr/local/lib/containers/zpool-creator/create-zpool`.
2. **A Service Definition**: Located at `/usr/local/etc/containers/zpool-creator.yaml`.

The service is configured to depend on the `zfs` extension and `configuration`
availability. It is idempotent; if the pools already exist, the service exits
successfully without doing anything.

## Usage

### Prerequisites

This extension requires the standard Sidero Labs `zfs` extension to be installed
on the Talos node, as it relies on the `zpool` binary provided by that
extension. The versions of the `zfs` extension and the `zpool` binary must be
compatible with the ZFS pool creation logic implemented in the extension.

### Building the Extension

Use the provided `Makefile` to build the extension image. The build process uses
a multi-stage Dockerfile to compile the Go binary and package it into a minimal
`scratch` image.

```sh
# Build the extension image
make build

# Push to your container registry
make push
```

Pushing requires a successful build, no failing tests, and a new tag.

### Talos Configuration

#### 1. Add the Extension

System extensions should be included at image creation time using the Talos
`imager` tool. Use the `--system-extension-image` flag to include this extension
and the required ZFS extension.

```sh
docker run -t --rm -v .:/work --privileged ghcr.io/siderolabs/imager:v1.13.4 \
  installer \
  --system-extension-image ghcr.io/siderolabs/zfs:2.4.1-v1.13.4 \
  --system-extension-image ghcr.io/containdk/talos-zpool-extension:latest
```

This will produce a `installer-amd64.tar` file containing the container image.
It can be loaded into docker using:

```sh
docker load -i installer-amd64.tar
```

Once loaded, re-tag the image to match your registry and push it:

```sh
docker tag ghcr.io/siderolabs/installer-base:v1.13.4 your-registry/talos-installer-image:v1.13.4
docker push your-registry/talos-installer-image:v1.13.4
```

Remember to match the talos versions.

#### 2. Configure the Service

Once the node is running with the extension, configure it by applying an
`ExtensionServiceConfig` document. This is where you specify the pools to create
using nested environment variables.

The extension will look for `ZPOOL_0_NAME`, `ZPOOL_1_NAME`, and so on, creating
a pool for each index it finds. If one pool fails, the extension will log the
error and continue to the next. It will exit with an error only after attempting
all configurations.

##### Example: Create two pools

```yaml
apiVersion: v1alpha1
kind: ExtensionServiceConfig
name: zpool-creator
environment:
  # First pool: a mirrored 'tank' mixing one explicit path and one dynamic model
  - ZPOOL_0_NAME=tank
  - ZPOOL_0_TYPE=mirror
  - ZPOOL_0_ASHIFT=12
  - ZPOOL_0_DISK_0_DEV=/dev/sda
  - ZPOOL_0_DISK_1_MODEL=Dell DC NVMe CD8*
  # Size filters are pool-wide constraints (all matched disks must meet them)
  - ZPOOL_0_SIZE_0=>=900GB
  - ZPOOL_0_SIZE_1=<=1.2TB

  # Second pool: a single-disk pool defined dynamically by disk model pattern
  - ZPOOL_1_NAME=data
  - ZPOOL_1_DISK_0_MODEL=SAMSUNG*

  # Global ASHIFT fallback (used if ZPOOL_<n>_ASHIFT is not set for a pool)
  - ZPOOL_ASHIFT=12
```

### Configuration Variables

The extension is configured by defining one or more pools using nested
environment variables. The process starts at pool index `0` and continues as long as
a `ZPOOL_<n>_NAME` is found.

For each pool `n` (e.g., `0`, `1`, `2`, ...), the following variables are used:

| Variable | Required? | Description |
| :--- | :--- | :--- |
| `ZPOOL_<n>_NAME` | **Yes** | The name of the ZFS pool to create (e.g., `ZPOOL_0_NAME=tank`). |
| `ZPOOL_<n>_TYPE` | No | The vdev type (`mirror`, `raidz`, `raidz1`, `raidz2`, `raidz3`, `draid`, etc.). If empty, disks are added as individual vdevs. |
| `ZPOOL_<n>_ASHIFT` | No | The `ashift` value for this specific pool. If not set, it falls back to the global `ZPOOL_ASHIFT` value. |
| `ZPOOL_<n>_DISK_<m>_DEV` | No | Explicit block device path for the `m`-th disk of pool `n` (e.g., `ZPOOL_0_DISK_0_DEV=/dev/sda`). |
| `ZPOOL_<n>_DISK_<m>_MODEL` | No | Dynamic model matching pattern for the `m`-th disk of pool `n` (e.g., `ZPOOL_0_DISK_1_MODEL=Dell DC NVMe CD8*`). Supports wildcards. |
| `ZPOOL_<n>_SIZE_<p>` | No | Indexed pool-wide mathematical disk size filters (e.g., `ZPOOL_0_SIZE_0=>=900GB`). All conditions must be met (logical AND). |

*Note: For each disk `m` in pool `n`, you must define either `ZPOOL_<n>_DISK_<m>_DEV` or `ZPOOL_<n>_DISK_<m>_MODEL`.*

### Dynamic Disk Selection by Model

Because block device names (like `/dev/nvme0n1`) are not guaranteed to be deterministic under Talos and can change during boot or installation, the extension supports selecting disks dynamically using their model name. This helps you avoid selecting or overwriting the disk used by Talos for its operating system.

To use dynamic selection, specify your disk models using the `ZPOOL_<pool>_DISK_<disk>_MODEL` variables:

```yaml
environment:
  # A mirrored pool created using the first two available, unpartitioned Dell CD8 NVMe disks
  - ZPOOL_0_NAME=tank
  - ZPOOL_0_TYPE=mirror
  - ZPOOL_0_DISK_0_MODEL=Dell DC NVMe CD8*
  - ZPOOL_0_DISK_1_MODEL=Dell DC NVMe CD8*
  - ZPOOL_0_ASHIFT=12
```

#### How it Works:
1. **Normalization**: The extension normalizes both your target model pattern and the sysfs model name (retrieved from `/sys/block/<dev>/device/model`) by converting them to lowercase and trimming surrounding whitespace. Characters like dashes and underscores are preserved exactly as they are.
2. **Wildcard & Substring Matching**: 
   - If the pattern contains wildcards (`*` or `?`), it performs a standard glob match. For example, `Dell*` matches any model starting with "Dell", and `*CD8*` matches any model containing "CD8".
   - If no wildcards are present, it falls back to a forgiving substring check. For example, `Samsung` matches any model containing "Samsung" anywhere in its name.
3. **Partition Detection**: The extension automatically scans `/sys/block` and skips any disk that has existing partitions (e.g., the operating system disk).
4. **Duplicate Prevention**: Each matching disk is tracked. If you specify multiple model entries (e.g., `Samsung*` and `Samsung*` to build a mirror), the extension will resolve them to distinct, unique physical disks.

### Disk Filtering by Size

You can filter disks dynamically by capacity using indexed `ZPOOL_<n>_SIZE_<p>` environment variables. This is highly recommended to filter out smaller system/boot disks or target specific ranges (e.g., only matching 1 TB NVMe SSDs).

#### Syntax & Behavior:
- Conditions are strings containing a comparison operator and a value with **no spaces** in-between (e.g., `ZPOOL_0_SIZE_0=>=900GB`).
- Multiple filters on the same pool act as a **logical AND** (e.g., `ZPOOL_0_SIZE_0=>=100GB` and `ZPOOL_0_SIZE_1=<=2TB` targets disks between 100 GB and 2 TB).
- **Supported Operators**: `<` (less than), `>` (greater than), `<=` (less than or equal), `>=` (greater than or equal), `=` or `==` (equal to).
- **Supported Units** (case-insensitive):
  - `B`: Bytes
  - `K`, `KB`, `KiB`: Kilobytes ($1024^1$)
  - `M`, `MB`, `MiB`: Megabytes ($1024^2$)
  - `G`, `GB`, `GiB`: Gigabytes ($1024^3$)
  - `T`, `TB`, `TiB`: Terabytes ($1024^4$)
- Fractional values are fully supported (e.g., `>=1.2TB`).

A global `ZPOOL_ASHIFT` can also be set as a default for all pools.

| Variable | Default | Description |
| :--- | :--- | :--- |
| `ZPOOL_ASHIFT` | `12` | The global `ashift` value to use if a pool-specific `ZPOOL_<n>_ASHIFT` is not defined. |

## Development

The creator is written in Go to ensure compatibility with the Talos environment. 
The source code and its Go module files are located in the `create-zpool/` directory.

- `create-zpool/main.go`: The source code for the creator binary.
- `zpool-creator.yaml`: The Talos service definition.
- `Dockerfile`: The multi-stage build definition.
