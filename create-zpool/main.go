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

// poolConfig holds the configuration for a single ZFS pool.
type poolConfig struct {
	Name   string   // Name of the ZFS pool (e.g., "tank").
	Type   string   // Type of the vdev (e.g., "mirror", "raidz", "draid"). Can be empty for single-disk vdevs.
	Disks  []string // List of disk paths or device nodes to be used in the pool (e.g., "/dev/sda", "/dev/sdb").
	Ashift string   // ashift property for the pool, specifying the sector size alignment (e.g., "12" for 4K).
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Talos ZFS Pool Extension: Starting ZFS Pool Creation")

	provider := &liveZFSProvider{}

	zpoolPath, err := provider.LookPath("zpool")
	if err != nil {
		slog.Error("zpool binary not found in PATH", "error", err, "PATH", os.Getenv("PATH"))
		os.Exit(1)
	}
	slog.Info("Found zpool binary", "path", zpoolPath)

	configs := parsePoolConfigs()
	if len(configs) == 0 {
		slog.Info("No pool configurations found (e.g., ZPOOL_NAME_0 is not set). Exiting cleanly.")
		os.Exit(0)
	}

	var allErrors []error
	for _, config := range configs {
		slog.Info("Processing pool configuration", "pool", config.Name)
		err := createPool(provider, zpoolPath, config)
		if err != nil {
			slog.Error("Failed to create pool", "pool", config.Name, "error", err)
			allErrors = append(allErrors, fmt.Errorf("pool %q: %w", config.Name, err))
		}
	}

	if len(allErrors) > 0 {
		slog.Error("One or more pools failed to create.", "error_count", len(allErrors))
		for _, e := range allErrors {
			slog.Error("Detailed error", "error", e)
		}
		os.Exit(1)
	}

	slog.Info("Talos ZFS Pool Extension: All pools processed successfully. Finished.")
}

// parsePoolConfigs reads indexed environment variables (ZPOOL_NAME_0, etc.)
// and returns a slice of PoolConfig structs.
func parsePoolConfigs() []poolConfig {
	var configs []poolConfig
	globalAshift := getEnv("ZPOOL_ASHIFT", defaultAshift)

	for i := range maxPools {
		poolNameKey := fmt.Sprintf("ZPOOL_NAME_%d", i)
		poolName := os.Getenv(poolNameKey)

		if poolName == "" {
			// This is the normal exit condition, no more pools are defined.
			break
		}

		poolDisksKey := fmt.Sprintf("ZPOOL_DISKS_%d", i)
		poolDisksStr := os.Getenv(poolDisksKey)

		poolTypeKey := fmt.Sprintf("ZPOOL_TYPE_%d", i)
		poolType := os.Getenv(poolTypeKey)

		poolAshiftKey := fmt.Sprintf("ZPOOL_ASHIFT_%d", i)
		ashift := getEnv(poolAshiftKey, globalAshift)

		config := poolConfig{
			Name:   poolName,
			Type:   poolType,
			Disks:  strings.Fields(poolDisksStr),
			Ashift: ashift,
		}
		configs = append(configs, config)
	}

	// After the loop, check if the reason for stopping was hitting the limit.
	if os.Getenv(fmt.Sprintf("ZPOOL_NAME_%d", maxPools)) != "" {
		slog.Warn("Reached the maximum number of pools allowed, ignoring further configurations.", "limit", maxPools)
	}

	return configs
}

// createPool handles the logic for creating a single ZFS pool.
func createPool(provider zfsProvider, zpoolPath string, config poolConfig) error {
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

	// Probe for specified disks
	slog.Info("Probing for specified disks", "pool", config.Name, "disks", config.Disks)
	var disksToUse []string
	for _, disk := range config.Disks {
		isBlock, err := provider.IsBlockDevice(disk)
		if err != nil {
			slog.Warn("Error checking device. Skipping.", "pool", config.Name, "device", disk, "error", err)
			continue
		}
		if isBlock {
			slog.Info("Found block device", "pool", config.Name, "device", disk)
			disksToUse = append(disksToUse, disk)
		} else {
			slog.Warn("Device is not a block device or does not exist. Skipping.", "pool", config.Name, "device", disk)
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
