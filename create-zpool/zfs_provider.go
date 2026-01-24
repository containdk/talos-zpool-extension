package main

import (
	"os"
	"os/exec"
)

// ZFSProvider defines an interface for interacting with ZFS and the filesystem,
// allowing for mocking in tests.
type ZFSProvider interface {
	LookPath(file string) (string, error)
	PoolExists(name, zpoolPath string) bool
	CreatePool(zpoolPath string, args []string) ([]byte, error)
	GetPoolStatus(name, zpoolPath string) ([]byte, error)
	IsBlockDevice(path string) (bool, error)
}

// LiveZFSProvider is the concrete implementation of ZFSProvider that executes
// real commands and interacts with the live filesystem.
type LiveZFSProvider struct{}

// LookPath wraps exec.LookPath.
func (p *LiveZFSProvider) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// PoolExists checks if a ZFS pool with the given name already exists.
func (p *LiveZFSProvider) PoolExists(name, zpoolPath string) bool {
	cmd := exec.Command(zpoolPath, "list", name)
	// We only care if the command succeeds (exit code 0), not about its output.
	return cmd.Run() == nil
}

// CreatePool executes the `zpool create` command.
func (p *LiveZFSProvider) CreatePool(zpoolPath string, args []string) ([]byte, error) {
	cmd := exec.Command(zpoolPath, args...)
	return cmd.CombinedOutput()
}

// GetPoolStatus executes the `zpool status` command.
func (p *LiveZFSProvider) GetPoolStatus(name, zpoolPath string) ([]byte, error) {
	cmd := exec.Command(zpoolPath, "status", name)
	return cmd.CombinedOutput()
}

// IsBlockDevice checks if the given path corresponds to a block device.
func (p *LiveZFSProvider) IsBlockDevice(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	// Check if it's a device and not a character device.
	isBlockDevice := info.Mode()&os.ModeDevice != 0 && info.Mode()&os.ModeCharDevice == 0
	return isBlockDevice, nil
}
