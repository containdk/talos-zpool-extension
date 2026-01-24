package main

import (
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

func main() {
	// Initialize slog with text handler for Talos console output
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Talos ZFS Pool Extension: Starting ZFS Pool Creation")

	// Check if zpool is available in PATH
	if _, err := exec.LookPath("zpool"); err != nil {
		slog.Error("zpool command not found in PATH. Make sure the ZFS extension is installed.", "error", err, "path", os.Getenv("PATH"))
		os.Exit(1)
	}

	// 1. Get configuration from environment variables
	zpoolName := getEnv("ZPOOL_NAME", "csi")
	zpoolType := os.Getenv("ZPOOL_TYPE")
	zpoolDisksStr := os.Getenv("ZPOOL_DISKS")
	ashift := getEnv("ASHIFT", "12")

	if zpoolDisksStr == "" {
		slog.Info("ZPOOL_DISKS is not set. No disks to process. Exiting cleanly.")
		os.Exit(0)
	}

	zpoolDisks := strings.Fields(zpoolDisksStr)

	// 2. Check if the pool already exists
	if poolExists(zpoolName) {
		slog.Info("ZFS pool already exists. Nothing to do.", "pool", zpoolName)
		os.Exit(0)
	}

	// 3. Probe for specified disks
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

	// 4. Create ZFS pool
	slog.Info("Creating ZFS pool", "pool", zpoolName, "ashift", ashift)

	args := []string{"create", "-o", "ashift=" + ashift, "-f", zpoolName}
	if zpoolType != "" {
		slog.Info("Using zpool type", "type", zpoolType)
		args = append(args, zpoolType)
	}
	args = append(args, disksToUse...)

	cmd := exec.Command("/usr/local/sbin/zpool", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	slog.Info("Running zpool command", "args", strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		slog.Error("Failed to create ZFS pool", "error", err, "pool", zpoolName)
		os.Exit(1)
	}

	slog.Info("ZFS pool created successfully", "pool", zpoolName)

	// Show status
	statusCmd := exec.Command("/usr/local/sbin/zpool", "status", zpoolName)
	statusCmd.Stdout = os.Stdout
	statusCmd.Stderr = os.Stderr
	_ = statusCmd.Run()

	slog.Info("Talos ZFS Pool Extension: Finished")
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func poolExists(name string) bool {
	cmd := exec.Command("/usr/local/sbin/zpool", "list", name)
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
