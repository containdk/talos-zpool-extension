package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

type MockZFSProvider struct {
	LookPathFunc      func(file string) (string, error)
	PoolExistsFunc    func(name, zpoolPath string) bool
	CreatePoolFunc    func(zpoolPath string, args []string) ([]byte, error)
	GetPoolStatusFunc func(name, zpoolPath string) ([]byte, error)
	IsBlockDeviceFunc func(path string) (bool, error)
}

func (m *MockZFSProvider) LookPath(file string) (string, error) {
	if m.LookPathFunc != nil {
		return m.LookPathFunc(file)
	}
	return "/fake/zpool", nil
}

func (m *MockZFSProvider) PoolExists(name, zpoolPath string) bool {
	if m.PoolExistsFunc != nil {
		return m.PoolExistsFunc(name, zpoolPath)
	}
	return false
}

func (m *MockZFSProvider) CreatePool(zpoolPath string, args []string) ([]byte, error) {
	if m.CreatePoolFunc != nil {
		return m.CreatePoolFunc(zpoolPath, args)
	}
	return []byte("Pool created successfully"), nil
}

func (m *MockZFSProvider) GetPoolStatus(name, zpoolPath string) ([]byte, error) {
	if m.GetPoolStatusFunc != nil {
		return m.GetPoolStatusFunc(name, zpoolPath)
	}
	return []byte("Pool is online"), nil
}

func (m *MockZFSProvider) IsBlockDevice(path string) (bool, error) {
	if m.IsBlockDeviceFunc != nil {
		return m.IsBlockDeviceFunc(path)
	}
	return true, nil
}

// --- Unit Tests for Validation Functions ---

func TestIsValidZpoolName(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  bool
	}{
		// Valid cases
		{"valid simple", "tank", true},
		{"valid with dash", "my-pool", true},
		{"valid with underscore", "my_pool", true},
		{"valid with dot", "my.pool", true},
		{"valid with colon", "a:b", true},
		{"valid with space", "my pool", true},
		{"valid alphanumeric", "p00l1", true},

		// Invalid cases
		{"empty string", "", false},
		{"starts with dash", "-tank", false},
		{"starts with dot", ".tank", false},
		{"contains invalid char", "ta&nk", false},
		{"contains path traversal", "../tank", false},
		{"is reserved name mirror", "mirror", false},
		{"is reserved name log", "log", false},
		{"is reserved name spare", "spare", false},
		{"starts with reserved prefix", "raidz-my-pool", false},
		{"starts with reserved prefix 2", "spare1", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := isValidZpoolName(tc.input)
			if got != tc.want {
				t.Errorf("isValidZpoolName(%q) = %v; want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsValidZpoolType(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty type", "", true},
		{"valid mirror", "mirror", true},
		{"valid raidz1", "raidz1", true},
		{"valid raidz2", "raidz2", true},
		{"valid raidz3", "raidz3", true},
		{"valid draid", "draid", true},
		{"invalid type", "raid0", false},
		{"misspelled", "miror", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := isValidZpoolType(tc.input)
			if got != tc.want {
				t.Errorf("isValidZpoolType(%q) = %v; want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsValidAshift(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid 9", "9", true},
		{"valid 12", "12", true},
		{"valid 13", "13", true},
		{"zero", "0", true},
		{"invalid string", "twelve", false},
		{"invalid float", "12.5", false},
		{"empty string", "", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := isValidAshift(tc.input)
			if got != tc.want {
				t.Errorf("isValidAshift(%q) = %v; want %v", tc.input, got, tc.want)
			}
		})
	}
}

// --- Fuzz Test ---

func FuzzIsValidZpoolName(f *testing.F) {
	// Add seed corpus from unit tests
	testCases := []string{"tank", "my-pool", "", "-tank", "mirror", "raidz-pool", "ta&nk"}
	for _, tc := range testCases {
		f.Add(tc)
	}

	f.Fuzz(func(t *testing.T, orig string) {
		// Run the function, it should not panic
		isValidZpoolName(orig)

		// A few simple invariants
		if strings.ContainsAny(orig, "!@#$%^&*()+=[]{}|\\;\"'<>,?/") {
			if isValidZpoolName(orig) {
				t.Errorf("fuzzer found a case where an invalid char was allowed: %q", orig)
			}
		}
		if strings.HasPrefix(orig, "-") || strings.HasPrefix(orig, ".") {
			if isValidZpoolName(orig) {
				t.Errorf("fuzzer found a case where an invalid prefix was allowed: %q", orig)
			}
		}
	})
}

// --- Integration-style Tests ---

func TestParsePoolConfigs(t *testing.T) {
	// Set environment variables for the test
	os.Setenv("ZPOOL_NAME_0", "tank0")
	os.Setenv("ZPOOL_DISKS_0", "/dev/sda /dev/sdb")
	os.Setenv("ZPOOL_TYPE_0", "mirror")
	os.Setenv("ASHIFT_0", "13")
	os.Setenv("ZPOOL_NAME_1", "tank1")
	os.Setenv("ZPOOL_DISKS_1", "/dev/sdc")
	// ZPOOL_TYPE_1 is intentionally omitted
	os.Setenv("ASHIFT", "12") // Global ashift

	// Clean up env vars after test
	defer func() {
		os.Unsetenv("ZPOOL_NAME_0")
		os.Unsetenv("ZPOOL_DISKS_0")
		os.Unsetenv("ZPOOL_TYPE_0")
		os.Unsetenv("ASHIFT_0")
		os.Unsetenv("ZPOOL_NAME_1")
		os.Unsetenv("ZPOOL_DISKS_1")
		os.Unsetenv("ASHIFT")
	}()

	configs := parsePoolConfigs()

	if len(configs) != 2 {
		t.Fatalf("parsePoolConfigs() returned %d configs, want 2", len(configs))
	}

	// Check config 0
	if configs[0].Name != "tank0" || configs[0].Type != "mirror" || configs[0].Ashift != "13" {
		t.Errorf("config 0 is incorrect: got %+v", configs[0])
	}
	if len(configs[0].Disks) != 2 || configs[0].Disks[0] != "/dev/sda" {
		t.Errorf("config 0 disks are incorrect: got %v", configs[0].Disks)
	}

	// Check config 1 (uses global ashift, empty type)
	if configs[1].Name != "tank1" || configs[1].Type != "" || configs[1].Ashift != "12" {
		t.Errorf("config 1 is incorrect: got %+v", configs[1])
	}
	if len(configs[1].Disks) != 1 || configs[1].Disks[0] != "/dev/sdc" {
		t.Errorf("config 1 disks are incorrect: got %v", configs[1].Disks)
	}
}

func TestParsePoolConfigs_Limit(t *testing.T) {
	// Set more environment variables than the MaxPools limit
	for i := 0; i <= MaxPools; i++ {
		os.Setenv(fmt.Sprintf("ZPOOL_NAME_%d", i), fmt.Sprintf("pool%d", i))
	}
	defer func() {
		for i := 0; i <= MaxPools; i++ {
			os.Unsetenv(fmt.Sprintf("ZPOOL_NAME_%d", i))
		}
	}()

	configs := parsePoolConfigs()

	if len(configs) != MaxPools {
		t.Fatalf("parsePoolConfigs() returned %d configs, want %d (MaxPools limit)", len(configs), MaxPools)
	}

	// Check if the last parsed pool is the one just before the limit
	expectedLastName := fmt.Sprintf("pool%d", MaxPools-1)
	actualLastName := configs[MaxPools-1].Name
	if actualLastName != expectedLastName {
		t.Errorf("Last parsed pool name is incorrect: got %q, want %q", actualLastName, expectedLastName)
	}
}

func TestCreatePool_Success(t *testing.T) {
	mockProvider := &MockZFSProvider{}
	config := PoolConfig{
		Name:   "goodpool",
		Type:   "mirror",
		Disks:  []string{"/dev/sda", "/dev/sdb"},
		Ashift: "12",
	}

	err := createPool(mockProvider, "/fake/zpool", config)
	if err != nil {
		t.Fatalf("createPool() returned an unexpected error: %v", err)
	}
}

func TestCreatePool_PartialFailure(t *testing.T) {
	mockProvider := &MockZFSProvider{
		CreatePoolFunc: func(zpoolPath string, args []string) ([]byte, error) {
			// Fail only for a specific pool
			if strings.Contains(strings.Join(args, " "), "badpool") {
				return []byte("Error output"), errors.New("zpool command failed")
			}
			return []byte("Success"), nil
		},
	}

	configs := []PoolConfig{
		{Name: "goodpool", Disks: []string{"/dev/sda"}, Ashift: "12"},
		{Name: "badpool", Disks: []string{"/dev/sdb"}, Ashift: "12"},
		{Name: "anothergoodpool", Disks: []string{"/dev/sdc"}, Ashift: "12"},
	}

	var allErrors []error
	for _, config := range configs {
		err := createPool(mockProvider, "/fake/zpool", config)
		if err != nil {
			allErrors = append(allErrors, fmt.Errorf("pool %q: %w", config.Name, err))
		}
	}

	if len(allErrors) != 1 {
		t.Fatalf("Expected 1 error, but got %d", len(allErrors))
	}
	if !strings.Contains(allErrors[0].Error(), "badpool") {
		t.Errorf("Error message does not contain the failed pool name: %v", allErrors[0])
	}
}

func TestCreatePool_DiskNotBlockDevice(t *testing.T) {
	mockProvider := &MockZFSProvider{
		IsBlockDeviceFunc: func(path string) (bool, error) {
			if path == "/dev/sdb" {
				return false, nil // This one is not a block device
			}
			return true, nil
		},
	}
	config := PoolConfig{
		Name:   "testpool",
		Disks:  []string{"/dev/sda", "/dev/sdb"},
		Ashift: "12",
	}

	// We need to capture the arguments passed to CreatePool to see what disks were used
	var usedDisks []string
	mockProvider.CreatePoolFunc = func(zpoolPath string, args []string) ([]byte, error) {
		// A bit of a hacky way to find the disk arguments
		for _, arg := range args {
			if strings.HasPrefix(arg, "/dev/") {
				usedDisks = append(usedDisks, arg)
			}
		}
		return nil, nil
	}

	err := createPool(mockProvider, "/fake/zpool", config)
	if err != nil {
		t.Fatalf("createPool() returned an unexpected error: %v", err)
	}

	if len(usedDisks) != 1 {
		t.Fatalf("Expected CreatePool to be called with 1 disk, but got %d", len(usedDisks))
	}
	if usedDisks[0] != "/dev/sda" {
		t.Errorf("Expected disk '/dev/sda' to be used, but got %v", usedDisks)
	}
}
