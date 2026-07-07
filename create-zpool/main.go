package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultPoolName = "tank"
	defaultAshift   = "12"
	maxPools        = 42 // Sanity limit for the number of pools to create.
)

// diskSpec defines a target disk declaration which can be defined by explicit path (dev) or dynamic query (model).
type diskSpec struct {
	Dev   string // Explicit block device path (e.g. "/dev/sda")
	Model string // Dynamic disk model query (e.g. "Dell DC NVMe CD8*")
}

// poolConfig holds the configuration for a single ZFS pool.
type poolConfig struct {
	Name        string     // Name of the ZFS pool (e.g., "tank").
	Type        string     // Type of the vdev (e.g., "mirror", "raidz", "draid"). Can be empty for single-disk vdevs.
	Disks       []diskSpec // List of ordered disk specifications.
	SizeFilters []string   // List of pool-wide size filter conditions.
	Ashift      string     // ashift property for the pool, specifying the sector size alignment (e.g., "12" for 4K).
}

// zfsConfig holds the configuration for a single ZFS dataset (filesystem or volume).
type zfsConfig struct {
	Name       string // Name of the dataset/volume (e.g., "tank/my-fs").
	Mountpoint string // Optional mountpoint property.
	VolSize    string // Optional volume size (only for volumes).
	Quota      string // Optional quota (only for filesystems).
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Talos ZFS Extension: Starting Configuration Probing")

	provider := &liveZFSProvider{}

	zpoolPath, err := provider.LookPath("zpool")
	if err != nil {
		slog.Error("zpool binary not found in PATH", "error", err, "PATH", os.Getenv("PATH"))
		os.Exit(1)
	}
	slog.Info("Found zpool binary", "path", zpoolPath)

	zfsPath, err := provider.LookPath("zfs")
	if err != nil {
		slog.Error("zfs binary not found in PATH", "error", err, "PATH", os.Getenv("PATH"))
		os.Exit(1)
	}
	slog.Info("Found zfs binary", "path", zfsPath)

	configs := parsePoolConfigs()
	zfsConfigs := parseZFSConfigs()

	if len(configs) == 0 && len(zfsConfigs) == 0 {
		slog.Info("No ZFS pools or dataset/volume configurations found. Exiting cleanly.")
		os.Exit(0)
	}

	var allErrors []error

	// 1. Process ZFS Pools
	if len(configs) > 0 {
		slog.Info("Processing ZFS Pool configurations", "pool_count", len(configs))
		usedDisks := make(map[string]bool)
		for _, config := range configs {
			slog.Info("Processing pool configuration", "pool", config.Name)
			err := createPool(provider, zpoolPath, config, usedDisks)
			if err != nil {
				slog.Error("Failed to create pool", "pool", config.Name, "error", err)
				allErrors = append(allErrors, fmt.Errorf("pool %q: %w", config.Name, err))
			}
		}
	}

	// 2. Process ZFS Datasets & Volumes
	if len(zfsConfigs) > 0 {
		slog.Info("Processing ZFS Dataset/Volume configurations", "count", len(zfsConfigs))
		for _, zfsConfig := range zfsConfigs {
			slog.Info("Processing ZFS dataset/volume configuration", "name", zfsConfig.Name)
			err := createDataset(provider, zfsPath, zfsConfig)
			if err != nil {
				slog.Error("Failed to create ZFS dataset/volume", "name", zfsConfig.Name, "error", err)
				allErrors = append(allErrors, fmt.Errorf("dataset/volume %q: %w", zfsConfig.Name, err))
			}
		}
	}

	if len(allErrors) > 0 {
		slog.Error("One or more configurations failed.", "error_count", len(allErrors))
		for _, e := range allErrors {
			slog.Error("Detailed error", "error", e)
		}
		os.Exit(1)
	}

	slog.Info("Talos ZFS Extension: All configurations processed successfully. Finished.")
}

// parsePoolConfigs reads nested indexed environment variables (ZPOOL_<n>_NAME, ZPOOL_<n>_DISK_<m>_DEV, etc.)
// and returns a slice of poolConfig structs.
func parsePoolConfigs() []poolConfig {
	var configs []poolConfig
	globalAshift := getEnv("ZPOOL_ASHIFT", defaultAshift)

	for i := range maxPools {
		poolNameKey := fmt.Sprintf("ZPOOL_%d_NAME", i)
		poolName := os.Getenv(poolNameKey)

		if poolName == "" {
			// This is the normal exit condition, no more pools are defined.
			break
		}

		poolTypeKey := fmt.Sprintf("ZPOOL_%d_TYPE", i)
		poolType := os.Getenv(poolTypeKey)

		poolAshiftKey := fmt.Sprintf("ZPOOL_%d_ASHIFT", i)
		ashift := getEnv(poolAshiftKey, globalAshift)

		config := poolConfig{
			Name:   poolName,
			Type:   poolType,
			Ashift: ashift,
		}

		// Parse nested disks
		for j := 0; ; j++ {
			devKey := fmt.Sprintf("ZPOOL_%d_DISK_%d_DEV", i, j)
			modelKey := fmt.Sprintf("ZPOOL_%d_DISK_%d_MODEL", i, j)

			devVal := os.Getenv(devKey)
			modelVal := os.Getenv(modelKey)

			if devVal == "" && modelVal == "" {
				break
			}

			config.Disks = append(config.Disks, diskSpec{
				Dev:   strings.TrimSpace(devVal),
				Model: strings.TrimSpace(modelVal),
			})
		}

		// Parse nested size filters
		for j := 0; ; j++ {
			sizeKey := fmt.Sprintf("ZPOOL_%d_SIZE_%d", i, j)
			sizeVal := os.Getenv(sizeKey)
			if sizeVal == "" {
				break
			}
			config.SizeFilters = append(config.SizeFilters, strings.TrimSpace(sizeVal))
		}

		configs = append(configs, config)
	}

	// After the loop, check if the reason for stopping was hitting the limit.
	if os.Getenv(fmt.Sprintf("ZPOOL_%d_NAME", maxPools)) != "" {
		slog.Warn("Reached the maximum number of pools allowed, ignoring further configurations.", "limit", maxPools)
	}

	return configs
}

// createPool handles the logic for creating a single ZFS pool.
func createPool(provider zfsProvider, zpoolPath string, config poolConfig, usedDisks map[string]bool) error {
	// Validate inputs
	if !isValidZpoolName(config.Name) {
		return fmt.Errorf("invalid name: %q", config.Name)
	}
	if !isValidZpoolType(config.Type) {
		return fmt.Errorf("invalid type: %q", config.Type)
	}
	if !isValidAshift(config.Ashift) {
		return fmt.Errorf("invalid ashift value: %q", config.Ashift)
	}
	if len(config.Disks) == 0 {
		slog.Info("No disks specified for pool. Skipping.", "pool", config.Name)
		return nil
	}

	// Check if the pool already exists
	if provider.PoolExists(config.Name, zpoolPath) {
		slog.Info("ZFS pool already exists. Nothing to do.", "pool", config.Name)
		return nil
	}

	// Parse size conditions if specified
	var sizeConds []sizeCondition
	for _, condStr := range config.SizeFilters {
		cond, err := parseSizeCondition(condStr)
		if err != nil {
			return fmt.Errorf("invalid size filter condition %q: %w", condStr, err)
		}
		sizeConds = append(sizeConds, cond)
	}

	// Probe for specified disks in the exact ordered declaration
	slog.Info("Probing specified disks", "pool", config.Name, "disks", config.Disks)
	var disksToUse []string
	for _, disk := range config.Disks {
		if disk.Dev != "" {
			canonicalDev, err := provider.EvalSymlinks(disk.Dev)
			if err != nil {
				slog.Warn("Error resolving symlink for device. Skipping.", "pool", config.Name, "device", disk.Dev, "error", err)
				continue
			}

			isBlock, err := provider.IsBlockDevice(canonicalDev)
			if err != nil {
				slog.Warn("Error checking device. Skipping.", "pool", config.Name, "device", canonicalDev, "error", err)
				continue
			}
			if isBlock {
				if usedDisks[canonicalDev] {
					slog.Warn("Device is already used by another configuration or disk. Skipping.", "pool", config.Name, "device", canonicalDev)
					continue
				}
				if !diskMatchesSize(provider, canonicalDev, sizeConds) {
					slog.Warn("Device size does not match size conditions. Skipping.", "pool", config.Name, "device", canonicalDev)
					continue
				}
				slog.Info("Found block device", "pool", config.Name, "device", canonicalDev)
				disksToUse = append(disksToUse, canonicalDev)
				usedDisks[canonicalDev] = true
			} else {
				slog.Warn("Device is not a block device or does not exist. Skipping.", "pool", config.Name, "device", canonicalDev)
			}
		} else if disk.Model != "" {
			resolved, err := provider.ResolveDiskByModel(disk.Model, sizeConds, usedDisks)
			if err != nil {
				slog.Warn("Error resolving disk by model. Skipping.", "pool", config.Name, "model", disk.Model, "error", err)
				continue
			}
			slog.Info("Resolved model to block device", "pool", config.Name, "model", disk.Model, "device", resolved)
			disksToUse = append(disksToUse, resolved)
			usedDisks[resolved] = true
		}
	}

	if len(disksToUse) == 0 {
		return errors.New("no usable block devices found from the provided list")
	}

	// Create ZFS pool
	slog.Info("Creating ZFS pool", "pool", config.Name, "ashift", config.Ashift, "type", config.Type)

	args := []string{"create", "-m", "/var/mnt/" + config.Name, "-o", "ashift=" + config.Ashift, config.Name}
	if config.Type != "" {
		args = append(args, config.Type)
	}
	args = append(args, disksToUse...)

	slog.Info("Running zpool command", "pool", config.Name, "args", strings.Join(args, " "))
	output, err := provider.CreatePool(zpoolPath, args)
	if err != nil {
		return fmt.Errorf("zpool create command failed: %w. Output: %s", err, string(output))
	}
	slog.Info("Zpool create command output", "pool", config.Name, "output", string(output))
	slog.Info("ZFS pool created successfully", "pool", config.Name)

	// Show status
	slog.Info("Showing pool status", "pool", config.Name)
	statusOutput, err := provider.GetPoolStatus(config.Name, zpoolPath)
	if err != nil {
		slog.Warn("Failed to show pool status, but pool may have been created.", "pool", config.Name, "error", err, "output", string(statusOutput))
	} else {
		slog.Info("Zpool status", "pool", config.Name, "status", string(statusOutput))
	}

	return nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// isValidZpoolName checks if the pool name is valid according to zpool(8).
// Pool names must begin with a letter, and can only contain alphanumeric characters
// as well as underscore (_), dash (-), colon (:), space ( ), and period (.).
// Reserved names (mirror, raidz, draid, spare, log) and names beginning with
// mirror, raidz, draid, and spare are not allowed.
func isValidZpoolName(name string) bool {
	if name == "" {
		return false
	}

	// Check for valid characters
	match, _ := regexp.MatchString(`^[a-zA-Z][a-zA-Z0-9_.: -]*$`, name)
	if !match {
		return false
	}

	// Check for reserved names
	reservedNames := map[string]bool{
		"mirror": true,
		"raidz":  true,
		"draid":  true,
		"spare":  true,
		"log":    true,
	}
	if reservedNames[name] {
		return false
	}

	// Check for reserved prefixes
	reservedPrefixes := []string{"mirror", "raidz", "draid", "spare"}
	for _, prefix := range reservedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}

	return true
}

// isValidZpoolType checks if the zpool type is one of the allowed values.
func isValidZpoolType(poolType string) bool {
	allowedTypes := map[string]bool{
		"":       true, // No type, for single disk or complex vdevs
		"mirror": true,
		"raidz":  true,
		"raidz1": true,
		"raidz2": true,
		"raidz3": true,
		"draid":  true,
		"draid1": true,
		"draid2": true,
		"draid3": true,
	}
	_, ok := allowedTypes[poolType]
	return ok
}

// isValidAshift checks if the ashift value is a valid integer.
func isValidAshift(ashift string) bool {
	_, err := strconv.Atoi(ashift)
	return err == nil
}

// diskMatchesSize checks if the block device meets all specified size conditions.
func diskMatchesSize(provider zfsProvider, path string, conds []sizeCondition) bool {
	if len(conds) == 0 {
		return true
	}
	size, err := provider.GetDiskSize(path)
	if err != nil {
		slog.Warn("Failed to get disk size for filtering", "device", path, "error", err)
		return false
	}
	for _, cond := range conds {
		if !cond.Matches(size) {
			slog.Info("Disk size does not match condition", "device", path, "size_bytes", size, "operator", cond.operator, "target_bytes", cond.target)
			return false
		}
	}
	return true
}

// parseZFSConfigs reads ZFS dataset/volume configuration from environment variables (ZFS_<n>_NAME, etc.)
func parseZFSConfigs() []zfsConfig {
	var configs []zfsConfig

	for i := range maxPools {
		nameKey := fmt.Sprintf("ZFS_%d_NAME", i)
		name := os.Getenv(nameKey)
		if name == "" {
			break
		}

		mountpointKey := fmt.Sprintf("ZFS_%d_MOUNTPOINT", i)
		volSizeKey := fmt.Sprintf("ZFS_%d_VOL_SIZE", i)
		quotaKey := fmt.Sprintf("ZFS_%d_QUOTA", i)

		configs = append(configs, zfsConfig{
			Name:       strings.TrimSpace(name),
			Mountpoint: strings.TrimSpace(os.Getenv(mountpointKey)),
			VolSize:    strings.TrimSpace(os.Getenv(volSizeKey)),
			Quota:      strings.TrimSpace(os.Getenv(quotaKey)),
		})
	}

	return configs
}

// createDataset handles the creation of a single ZFS filesystem or volume (zvol).
func createDataset(provider zfsProvider, zfsPath string, config zfsConfig) error {
	if config.Name == "" {
		return fmt.Errorf("ZFS dataset name cannot be empty")
	}

	if config.VolSize != "" && config.Quota != "" {
		return fmt.Errorf("VOL_SIZE and QUOTA are mutually exclusive (cannot define both on %s)", config.Name)
	}

	// Check if the dataset already exists
	if provider.DatasetExists(config.Name, zfsPath) {
		slog.Info("ZFS dataset/volume already exists. Nothing to do.", "name", config.Name)
		return nil
	}

	slog.Info("Creating ZFS dataset/volume", "name", config.Name)

	var args []string
	args = append(args, "create")

	if config.VolSize != "" {
		// It's a volume (zvol)
		if config.Mountpoint != "" {
			return fmt.Errorf("MOUNTPOINT is not supported for ZFS volumes (zvols) like %s", config.Name)
		}
		args = append(args, "-V", config.VolSize, config.Name)
	} else {
		// It's a filesystem (dataset)
		if config.Mountpoint != "" {
			args = append(args, "-o", "mountpoint="+config.Mountpoint)
		}
		if config.Quota != "" {
			args = append(args, "-o", "quota="+config.Quota)
		}
		args = append(args, config.Name)
	}

	output, err := provider.CreateDataset(zfsPath, args)
	if err != nil {
		return fmt.Errorf("failed to create ZFS dataset/volume %s: %w (output: %q)", config.Name, err, string(output))
	}

	slog.Info("ZFS dataset/volume created successfully", "name", config.Name)
	return nil
}
