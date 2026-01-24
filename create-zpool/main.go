package main

import (
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

const defaultPoolName = "tank"

// Global variable for zpool path to be used by helper functions.
var zpoolPath string

func main() {
	// Initialize slog with text handler for Talos console output
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Talos ZFS Pool Extension: Starting ZFS Pool Creation")

	// Find zpool binary in PATH
	var err error
	zpoolPath, err = exec.LookPath("zpool")
	if err != nil {
		slog.Error("zpool binary not found in PATH", "error", err)
		os.Exit(1)
	}
	slog.Info("Found zpool binary", "path", zpoolPath)

	// 1. Get configuration from environment variables
	zpoolName := getEnv("ZPOOL_NAME", defaultPoolName)
	zpoolType := os.Getenv("ZPOOL_TYPE")
	zpoolDisksStr := os.Getenv("ZPOOL_DISKS")
	ashift := getEnv("ASHIFT", "12")

	// 2. Validate inputs for security
	if !isValidZpoolName(zpoolName) {
		slog.Error("Invalid ZPOOL_NAME. See zpool-create(8) for naming rules. Name must start with a letter, contain valid characters (alphanumeric, '_', '-', '.', ':', ' '), and not be a reserved word.", "name", zpoolName)
		os.Exit(1)
	}
	if !isValidZpoolType(zpoolType) {
		slog.Error("Invalid ZPOOL_TYPE. If specified, must be one of mirror, raidz, raidz1, raidz2, raidz3, draid, draid1, draid2, draid3.", "type", zpoolType)
		os.Exit(1)
	}
	if !isValidAshift(ashift) {
		slog.Error("Invalid ASHIFT value. Must be an integer.", "ashift", ashift)
		os.Exit(1)
	}

	if zpoolDisksStr == "" {
		slog.Info("ZPOOL_DISKS is not set. No disks to process. Exiting cleanly.")
		os.Exit(0)
	}

	zpoolDisks := strings.Fields(zpoolDisksStr)

	// 3. Check if the pool already exists
	if poolExists(zpoolName) {
		slog.Info("ZFS pool already exists. Nothing to do.", "pool", zpoolName)
		os.Exit(0)
	}

	// 4. Probe for specified disks
	slog.Info("Probing for specified disks", "disks", zpoolDisks)
	var disksToUse []string
	for _, disk := range zpoolDisks {
		if isBlockDevice(disk) {
			slog.Info("Found block device", "device", disk)
			disksToUse = append(disksToUse, disk)
		} else {
			slog.Warn("Device is not a block device or does not exist. Skipping.", "device", disk)
		}
	}

	if len(disksToUse) == 0 {
		slog.Info("No usable disks found from the list. Exiting cleanly.", "input_disks", zpoolDisksStr)
		os.Exit(0)
	}

	// 5. Create ZFS pool
	slog.Info("Creating ZFS pool", "pool", zpoolName, "ashift", ashift)

	args := []string{"create", "-m", "/var/mnt/" + zpoolName, "-o", "ashift=" + ashift, zpoolName}
	if zpoolType != "" {
		slog.Info("Using zpool type", "type", zpoolType)
		args = append(args, zpoolType)
	}
	args = append(args, disksToUse...)

	cmd := exec.Command(zpoolPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	slog.Info("Running zpool command", "args", strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		slog.Error("Failed to create ZFS pool", "error", err, "pool", zpoolName)
		os.Exit(1)
	}

	slog.Info("ZFS pool created successfully", "pool", zpoolName)

	// Show status
	slog.Info("Showing pool status", "pool", zpoolName)
	statusCmd := exec.Command(zpoolPath, "status", zpoolName)
	statusCmd.Stdout = os.Stdout
	statusCmd.Stderr = os.Stderr
	if err := statusCmd.Run(); err != nil {
		slog.Warn("Failed to show pool status. The pool might have been created but something is wrong.", "error", err, "pool", zpoolName)
	}

	slog.Info("Talos ZFS Pool Extension: Finished")
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func poolExists(name string) bool {
	// Assumes zpoolPath has been set in main()
	cmd := exec.Command(zpoolPath, "list", name)
	return cmd.Run() == nil
}

func isBlockDevice(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	// Check if it's a block device
	return info.Mode()&os.ModeDevice != 0 && info.Mode()&os.ModeCharDevice == 0
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

	// 1. Check for valid characters
	match, _ := regexp.MatchString(`^[a-zA-Z][a-zA-Z0-9_.: -]*$`, name)
	if !match {
		return false
	}

	// 2. Check for reserved names
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

	// 3. Check for reserved prefixes
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
