package main

import (
	"os"
	"os/exec"
)

// zfsProvider defines an interface for interacting with ZFS and the filesystem,
// allowing for mocking in tests.
type zfsProvider interface {
	// LookPath searches for a binary in the system's PATH.
	LookPath(file string) (string, error)
	// PoolExists checks if a ZFS pool with the given name already exists.
	PoolExists(name, zpoolPath string) bool
	// CreatePool executes the `zpool create` command with the given arguments.
	// It returns the combined stdout/stderr output and any execution error.
	CreatePool(zpoolPath string, args []string) ([]byte, error)
	// GetPoolStatus executes the `zpool status` command for the given pool.
	// It returns the combined stdout/stderr output and any execution error.
	GetPoolStatus(name, zpoolPath string) ([]byte, error)
	// IsBlockDevice checks if the given path corresponds to a block device.
	IsBlockDevice(path string) (bool, error)
}

// liveZFSProvider is the concrete implementation of ZFSProvider that executes
// real commands and interacts with the live filesystem.
type liveZFSProvider struct{}

// LookPath wraps exec.LookPath.
func (p *liveZFSProvider) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// PoolExists checks if a ZFS pool with the given name already exists.
func (p *liveZFSProvider) PoolExists(name, zpoolPath string) bool {
	cmd := exec.Command(zpoolPath, "list", name)
	// We only care if the command succeeds (exit code 0), not about its output.
	return cmd.Run() == nil
}

// CreatePool creates a zpool using the `zpool create` command.
func (p *liveZFSProvider) CreatePool(zpoolPath string, args []string) ([]byte, error) {
	cmd := exec.Command(zpoolPath, args...)
	return cmd.CombinedOutput()
}

// GetPoolStatus returns the status of a ZFS pool using the `zpool status` command.
func (p *liveZFSProvider) GetPoolStatus(name, zpoolPath string) ([]byte, error) {
	cmd := exec.Command(zpoolPath, "status", name)
	return cmd.CombinedOutput()
}

// IsBlockDevice checks if the given path corresponds to a block device.
func (p *liveZFSProvider) IsBlockDevice(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	// Check if it's a device and not a character device.
	isBlockDevice := info.Mode()&os.ModeDevice != 0 && info.Mode()&os.ModeCharDevice == 0
	return isBlockDevice, nil
}
