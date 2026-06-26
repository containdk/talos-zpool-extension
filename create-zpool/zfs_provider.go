package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// sizeCondition represents a mathematical size condition.
type sizeCondition struct {
	operator string // "<", ">", "<=", ">=", "=", "=="
	target   uint64
}

// Matches evaluates if a size matches this condition.
func (c sizeCondition) Matches(size uint64) bool {
	switch c.operator {
	case "<":
		return size < c.target
	case ">":
		return size > c.target
	case "<=":
		return size <= c.target
	case ">=":
		return size >= c.target
	case "=", "==":
		return size == c.target
	}
	return false
}

// parseSizeInBytes converts a human-readable size string (e.g. "10GB", "1.2TB", "1000") to bytes.
func parseSizeInBytes(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	// Find the boundary between digits/dots and the unit
	numEnd := 0
	for numEnd < len(s) {
		r := s[numEnd]
		if (r >= '0' && r <= '9') || r == '.' {
			numEnd++
		} else {
			break
		}
	}

	if numEnd == 0 {
		return 0, fmt.Errorf("invalid size format: %q (no numeric prefix)", s)
	}

	numStr := s[:numEnd]
	unitStr := strings.TrimSpace(strings.ToUpper(s[numEnd:]))

	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse numeric size %q: %w", numStr, err)
	}

	var multiplier float64 = 1
	switch unitStr {
	case "", "B":
		multiplier = 1
	case "K", "KB", "KIB":
		multiplier = 1024
	case "M", "MB", "MIB":
		multiplier = 1024 * 1024
	case "G", "GB", "GIB":
		multiplier = 1024 * 1024 * 1024
	case "T", "TB", "TIB":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown size unit %q", unitStr)
	}

	return uint64(val * multiplier), nil
}

// parseSizeCondition parses a comparison condition like ">=100GB" or "<2TB".
func parseSizeCondition(s string) (sizeCondition, error) {
	s = strings.TrimSpace(s)
	var op string
	var sizeStr string

	if strings.HasPrefix(s, "<=") {
		op = "<="
		sizeStr = s[2:]
	} else if strings.HasPrefix(s, ">=") {
		op = ">="
		sizeStr = s[2:]
	} else if strings.HasPrefix(s, "==") {
		op = "=="
		sizeStr = s[2:]
	} else if strings.HasPrefix(s, "<") {
		op = "<"
		sizeStr = s[1:]
	} else if strings.HasPrefix(s, ">") {
		op = ">"
		sizeStr = s[1:]
	} else if strings.HasPrefix(s, "=") {
		op = "="
		sizeStr = s[1:]
	} else {
		return sizeCondition{}, fmt.Errorf("invalid size condition %q: missing operator (<, >, <=, >=, =)", s)
	}

	target, err := parseSizeInBytes(sizeStr)
	if err != nil {
		return sizeCondition{}, fmt.Errorf("failed to parse size in %q: %w", s, err)
	}

	return sizeCondition{operator: op, target: target}, nil
}

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
	// ResolveDiskByModel scans /sys/block to find a disk matching the model
	// that is unpartitioned, meets size requirements, and not already marked as used.
	ResolveDiskByModel(model string, sizeConds []sizeCondition, usedDisks map[string]bool) (string, error)
	// GetDiskSize returns the size of the block device at the given path in bytes.
	GetDiskSize(path string) (uint64, error)
	// EvalSymlinks evaluates any symbolic links to return the canonical path.
	EvalSymlinks(path string) (string, error)
}

// liveZFSProvider is the concrete implementation of ZFSProvider that executes
// real commands and interacts with the live filesystem.
type liveZFSProvider struct{}

// LookPath wraps exec.LookPath.
func (p *liveZFSProvider) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// EvalSymlinks wraps filepath.EvalSymlinks.
func (p *liveZFSProvider) EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

// PoolExists checks if a ZFS pool with the given name already exists.
func (p *liveZFSProvider) PoolExists(name, zpoolPath string) bool {
	// #nosec G204: Intentionally executing system binary with dynamic pool name
	cmd := exec.Command(zpoolPath, "list", name)
	// We only care if the command succeeds (exit code 0), not about its output.
	return cmd.Run() == nil
}

// CreatePool creates a zpool using the `zpool create` command.
func (p *liveZFSProvider) CreatePool(zpoolPath string, args []string) ([]byte, error) {
	// #nosec G204: Intentionally executing system binary with user-configured arguments
	cmd := exec.Command(zpoolPath, args...)
	return cmd.CombinedOutput()
}

// GetPoolStatus returns the status of a ZFS pool using the `zpool status` command.
func (p *liveZFSProvider) GetPoolStatus(name, zpoolPath string) ([]byte, error) {
	// #nosec G204: Intentionally executing system binary with dynamic pool name
	cmd := exec.Command(zpoolPath, "status", name)
	return cmd.CombinedOutput()
}

// IsBlockDevice checks if the given path corresponds to a block device.
func (p *liveZFSProvider) IsBlockDevice(path string) (bool, error) {
	// #nosec G304: Intentionally statting user-provided device path node
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	// Check if it's a device and not a character device.
	isBlockDevice := info.Mode()&os.ModeDevice != 0 && info.Mode()&os.ModeCharDevice == 0
	return isBlockDevice, nil
}

// GetDiskSize returns the size of the block device at the given path in bytes.
func (p *liveZFSProvider) GetDiskSize(path string) (uint64, error) {
	realPath, err := p.EvalSymlinks(path)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve symlink for %s: %w", path, err)
	}
	devName := filepath.Base(realPath)
	sizeFile := filepath.Join("/sys/class/block", devName, "size")

	// #nosec G304: Intentionally reading disk size from sysfs
	sizeBytes, err := os.ReadFile(sizeFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read disk size file for %s: %w", devName, err)
	}
	blocksStr := strings.TrimSpace(string(sizeBytes))
	blocks, err := strconv.ParseUint(blocksStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse disk size %q: %w", blocksStr, err)
	}
	return blocks * 512, nil
}

var sysBlockPath = "/sys/block"

// ResolveDiskByModel scans /sys/block to find a disk matching the model
// that is unpartitioned, matches size restrictions, and not already marked as used.
func (p *liveZFSProvider) ResolveDiskByModel(targetModel string, sizeConds []sizeCondition, usedDisks map[string]bool) (string, error) {
	// #nosec G304: Intentionally reading sysBlockPath directory
	entries, err := os.ReadDir(sysBlockPath)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", sysBlockPath, err)
	}

	normalizedTarget := normalizeModel(targetModel)

	for _, entry := range entries {
		devName := entry.Name()

		// Skip loop, ram, and other virtual devices that won't have a model
		if strings.HasPrefix(devName, "loop") || strings.HasPrefix(devName, "ram") || strings.HasPrefix(devName, "dm-") {
			continue
		}

		devPath := filepath.Join("/dev", devName)
		if usedDisks[devPath] {
			continue
		}

		// Skip read-only devices
		roFile := filepath.Join(sysBlockPath, devName, "ro")
		// #nosec G304: Intentionally reading disk read-only status from sysfs
		roBytes, err := os.ReadFile(roFile)
		if err == nil {
			if strings.TrimSpace(string(roBytes)) == "1" {
				continue
			}
		}

		// Read disk model securely using root (G304 avoided)
		modelFile := filepath.Join(sysBlockPath, devName, "device", "model")
		// #nosec G304: Intentionally reading disk model from sysfs
		modelBytes, err := os.ReadFile(modelFile)
		if err != nil {
			// Skip if there's no model file (e.g. virtual disks, loop devices)
			continue
		}

		sysfsModel := strings.TrimSpace(string(modelBytes))
		normalizedSysfs := normalizeModel(sysfsModel)

		matched := false
		if strings.Contains(normalizedTarget, "*") || strings.Contains(normalizedTarget, "?") {
			var err error
			matched, err = filepath.Match(normalizedTarget, normalizedSysfs)
			if err != nil {
				continue
			}
		} else {
			matched = strings.Contains(normalizedSysfs, normalizedTarget)
		}

		if !matched {
			continue
		}

		// Check size conditions if any are specified securely using root (G304 avoided)
		if len(sizeConds) > 0 {
			sizeFile := filepath.Join(sysBlockPath, devName, "size")
			// #nosec G304: Intentionally reading disk size from sysfs
			sizeBytes, err := os.ReadFile(sizeFile)
			if err != nil {
				continue
			}
			blocksStr := strings.TrimSpace(string(sizeBytes))
			blocks, err := strconv.ParseUint(blocksStr, 10, 64)
			if err != nil {
				continue
			}
			size := blocks * 512

			matchesAll := true
			for _, cond := range sizeConds {
				if !cond.Matches(size) {
					matchesAll = false
					break
				}
			}
			if !matchesAll {
				continue
			}
		}

		// Check if the disk is partitioned.
		// A partition in /sys/block/<devName>/ is a subdirectory containing a file named "partition" securely using root (G304 avoided)
		devDir := filepath.Join(sysBlockPath, devName)
		// #nosec G304: Intentionally reading block device directory from sysfs
		subEntries, err := os.ReadDir(devDir)
		if err != nil {
			continue
		}

		isPartitioned := false
		for _, subEntry := range subEntries {
			if subEntry.IsDir() {
				partFile := filepath.Join(devDir, subEntry.Name(), "partition")
				// #nosec G304: Intentionally statting partition file under sysfs
				if _, err := os.Stat(partFile); err == nil {
					isPartitioned = true
					break
				}
			}
		}

		if isPartitioned {
			continue
		}

		// Found a matching, unpartitioned, unused disk!
		return devPath, nil
	}

	return "", fmt.Errorf("no unpartitioned, unused disk found matching model %q with the requested size conditions", targetModel)
}

// normalizeModel normalizes a model string to make matches more robust.
// It converts to lower case and trims surrounding whitespace.
func normalizeModel(m string) string {
	m = strings.ReplaceAll(m, "\x00", "")
	return strings.TrimSpace(strings.ToLower(m))
}
