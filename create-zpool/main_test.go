package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockZFSProvider struct {
	LookPathFunc           func(file string) (string, error)
	PoolExistsFunc         func(name, zpoolPath string) bool
	CreatePoolFunc         func(zpoolPath string, args []string) ([]byte, error)
	GetPoolStatusFunc      func(name, zpoolPath string) ([]byte, error)
	IsBlockDeviceFunc      func(path string) (bool, error)
	ResolveDiskByModelFunc func(model string, sizeConds []sizeCondition, usedDisks map[string]bool) (string, error)
	GetDiskSizeFunc        func(path string) (uint64, error)
}

func (m *mockZFSProvider) LookPath(file string) (string, error) {
	if m.LookPathFunc != nil {
		return m.LookPathFunc(file)
	}
	return "/fake/zpool", nil
}

func (m *mockZFSProvider) PoolExists(name, zpoolPath string) bool {
	if m.PoolExistsFunc != nil {
		return m.PoolExistsFunc(name, zpoolPath)
	}
	return false
}

func (m *mockZFSProvider) CreatePool(zpoolPath string, args []string) ([]byte, error) {
	if m.CreatePoolFunc != nil {
		return m.CreatePoolFunc(zpoolPath, args)
	}
	return []byte("Pool created successfully"), nil
}

func (m *mockZFSProvider) GetPoolStatus(name, zpoolPath string) ([]byte, error) {
	if m.GetPoolStatusFunc != nil {
		return m.GetPoolStatusFunc(name, zpoolPath)
	}
	return []byte("Pool is online"), nil
}

func (m *mockZFSProvider) IsBlockDevice(path string) (bool, error) {
	if m.IsBlockDeviceFunc != nil {
		return m.IsBlockDeviceFunc(path)
	}
	return true, nil
}

func (m *mockZFSProvider) ResolveDiskByModel(model string, sizeConds []sizeCondition, usedDisks map[string]bool) (string, error) {
	if m.ResolveDiskByModelFunc != nil {
		return m.ResolveDiskByModelFunc(model, sizeConds, usedDisks)
	}
	// Default mock: return a dummy path based on model name
	return "/dev/fake-" + model, nil
}

func (m *mockZFSProvider) GetDiskSize(path string) (uint64, error) {
	if m.GetDiskSizeFunc != nil {
		return m.GetDiskSizeFunc(path)
	}
	// Default size is 1 TB (well within standard sizes)
	return 1024 * 1024 * 1024 * 1024, nil
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
	os.Setenv("ZPOOL_0_NAME", "tank0")
	os.Setenv("ZPOOL_0_TYPE", "mirror")
	os.Setenv("ZPOOL_0_ASHIFT", "13")
	os.Setenv("ZPOOL_0_DISK_0_DEV", "/dev/sda")
	os.Setenv("ZPOOL_0_DISK_1_DEV", "/dev/sdb")
	os.Setenv("ZPOOL_0_SIZE_0", ">=900GB")

	os.Setenv("ZPOOL_1_NAME", "tank1")
	os.Setenv("ZPOOL_1_DISK_0_MODEL", "Samsung*")
	os.Setenv("ZPOOL_1_DISK_1_MODEL", "Dell*")
	os.Setenv("ZPOOL_ASHIFT", "12") // Global ashift

	// Clean up env vars after test
	defer func() {
		os.Unsetenv("ZPOOL_0_NAME")
		os.Unsetenv("ZPOOL_0_TYPE")
		os.Unsetenv("ZPOOL_0_ASHIFT")
		os.Unsetenv("ZPOOL_0_DISK_0_DEV")
		os.Unsetenv("ZPOOL_0_DISK_1_DEV")
		os.Unsetenv("ZPOOL_0_SIZE_0")

		os.Unsetenv("ZPOOL_1_NAME")
		os.Unsetenv("ZPOOL_1_DISK_0_MODEL")
		os.Unsetenv("ZPOOL_1_DISK_1_MODEL")
		os.Unsetenv("ZPOOL_ASHIFT")
	}()

	configs := parsePoolConfigs()

	if len(configs) != 2 {
		t.Fatalf("parsePoolConfigs() returned %d configs, want 2", len(configs))
	}

	// Check config 0 (by dev)
	if configs[0].Name != "tank0" || configs[0].Type != "mirror" || configs[0].Ashift != "13" {
		t.Errorf("config 0 is incorrect: got %+v", configs[0])
	}
	if len(configs[0].Disks) != 2 || configs[0].Disks[0].Dev != "/dev/sda" || configs[0].Disks[1].Dev != "/dev/sdb" {
		t.Errorf("config 0 disks are incorrect: got %v", configs[0].Disks)
	}
	if len(configs[0].SizeFilters) != 1 || configs[0].SizeFilters[0] != ">=900GB" {
		t.Errorf("config 0 size filters are incorrect: got %v", configs[0].SizeFilters)
	}

	// Check config 1 (by model, uses global ashift, empty type)
	if configs[1].Name != "tank1" || configs[1].Type != "" || configs[1].Ashift != "12" {
		t.Errorf("config 1 is incorrect: got %+v", configs[1])
	}
	if len(configs[1].Disks) != 2 || configs[1].Disks[0].Model != "Samsung*" || configs[1].Disks[1].Model != "Dell*" {
		t.Errorf("config 1 disks are incorrect: got %v", configs[1].Disks)
	}
}

func TestParsePoolConfigs_Limit(t *testing.T) {
	// Set more environment variables than the MaxPools limit
	for i := 0; i <= maxPools; i++ {
		os.Setenv(fmt.Sprintf("ZPOOL_%d_NAME", i), fmt.Sprintf("pool%d", i))
	}
	defer func() {
		for i := 0; i <= maxPools; i++ {
			os.Unsetenv(fmt.Sprintf("ZPOOL_%d_NAME", i))
		}
	}()

	configs := parsePoolConfigs()

	if len(configs) != maxPools {
		t.Fatalf("parsePoolConfigs() returned %d configs, want %d (MaxPools limit)", len(configs), maxPools)
	}

	// Check if the last parsed pool is the one just before the limit
	expectedLastName := fmt.Sprintf("pool%d", maxPools-1)
	actualLastName := configs[maxPools-1].Name
	if actualLastName != expectedLastName {
		t.Errorf("Last parsed pool name is incorrect: got %q, want %q", actualLastName, expectedLastName)
	}
}

func TestCreatePool_Success(t *testing.T) {
	mockProvider := &mockZFSProvider{}
	config := poolConfig{
		Name:   "goodpool",
		Type:   "mirror",
		Disks:  []diskSpec{{Dev: "/dev/sda"}, {Dev: "/dev/sdb"}},
		Ashift: "12",
	}

	usedDisks := make(map[string]bool)
	err := createPool(mockProvider, "/fake/zpool", config, usedDisks)
	if err != nil {
		t.Fatalf("createPool() returned an unexpected error: %v", err)
	}

	if !usedDisks["/dev/sda"] || !usedDisks["/dev/sdb"] {
		t.Errorf("Expected disks to be marked as used, but got %v", usedDisks)
	}
}

func TestCreatePool_PartialFailure(t *testing.T) {
	mockProvider := &mockZFSProvider{
		CreatePoolFunc: func(zpoolPath string, args []string) ([]byte, error) {
			// Fail only for a specific pool
			if strings.Contains(strings.Join(args, " "), "badpool") {
				return []byte("Error output"), errors.New("zpool command failed")
			}
			return []byte("Success"), nil
		},
	}

	configs := []poolConfig{
		{Name: "goodpool", Disks: []diskSpec{{Dev: "/dev/sda"}}, Ashift: "12"},
		{Name: "badpool", Disks: []diskSpec{{Dev: "/dev/sdb"}}, Ashift: "12"},
		{Name: "anothergoodpool", Disks: []diskSpec{{Dev: "/dev/sdc"}}, Ashift: "12"},
	}

	usedDisks := make(map[string]bool)
	var allErrors []error
	for _, config := range configs {
		err := createPool(mockProvider, "/fake/zpool", config, usedDisks)
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
	mockProvider := &mockZFSProvider{
		IsBlockDeviceFunc: func(path string) (bool, error) {
			if path == "/dev/sdb" {
				return false, nil // This one is not a block device
			}
			return true, nil
		},
	}
	config := poolConfig{
		Name:   "testpool",
		Disks:  []diskSpec{{Dev: "/dev/sda"}, {Dev: "/dev/sdb"}},
		Ashift: "12",
	}

	// We need to capture the arguments passed to CreatePool to see what disks were used
	var createPoolDisks []string
	mockProvider.CreatePoolFunc = func(zpoolPath string, args []string) ([]byte, error) {
		// A bit of a hacky way to find the disk arguments
		for _, arg := range args {
			if strings.HasPrefix(arg, "/dev/") {
				createPoolDisks = append(createPoolDisks, arg)
			}
		}
		return nil, nil
	}

	usedDisks := make(map[string]bool)
	err := createPool(mockProvider, "/fake/zpool", config, usedDisks)
	if err != nil {
		t.Fatalf("createPool() returned an unexpected error: %v", err)
	}

	if len(createPoolDisks) != 1 {
		t.Fatalf("Expected CreatePool to be called with 1 disk, but got %d", len(createPoolDisks))
	}
	if createPoolDisks[0] != "/dev/sda" {
		t.Errorf("Expected disk '/dev/sda' to be used, but got %v", createPoolDisks)
	}
}

func TestLiveZFSProvider_ResolveDiskByModel(t *testing.T) {
	// Create a temporary directory to mock /sys/block
	tmpDir := t.TempDir()

	// Backup and restore sysBlockPath
	oldSysBlockPath := sysBlockPath
	sysBlockPath = tmpDir
	t.Cleanup(func() {
		sysBlockPath = oldSysBlockPath
	})

	provider := &liveZFSProvider{}

	// Scenario 1: Setup a matching, unpartitioned disk "nvme1n1"
	nvme1Dir := filepath.Join(tmpDir, "nvme1n1")
	if err := os.MkdirAll(filepath.Join(nvme1Dir, "device"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nvme1Dir, "device", "model"), []byte("Dell DC NVMe CD8 U.2 960GB"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Size file: 960 GB is 1875000000 512-byte blocks
	if err := os.WriteFile(filepath.Join(nvme1Dir, "size"), []byte("1875000000"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Scenario 2: Setup a matching, partitioned disk "nvme0n1"
	nvme0Dir := filepath.Join(tmpDir, "nvme0n1")
	if err := os.MkdirAll(filepath.Join(nvme0Dir, "device"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nvme0Dir, "device", "model"), []byte("Dell DC NVMe CD8 U.2 960GB"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nvme0Dir, "size"), []byte("1875000000"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Add partition subdirectory and partition file
	partDir := filepath.Join(nvme0Dir, "nvme0n1p1")
	if err := os.MkdirAll(partDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partDir, "partition"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Scenario 3: Setup a disk with non-matching model "sda"
	sdaDir := filepath.Join(tmpDir, "sda")
	if err := os.MkdirAll(filepath.Join(sdaDir, "device"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sdaDir, "device", "model"), []byte("SAMSUNG_MZ7WD480HAGP"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 480 GB is 937500000 blocks
	if err := os.WriteFile(filepath.Join(sdaDir, "size"), []byte("937500000"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("Match model and skip partitioned/non-matching", func(t *testing.T) {
		usedDisks := make(map[string]bool)
		// Should match nvme1n1, skip sda (non-matching) and nvme0n1 (partitioned)
		resolved, err := provider.ResolveDiskByModel("Dell DC NVMe", nil, usedDisks)
		if err != nil {
			t.Fatalf("Expected to resolve disk, got error: %v", err)
		}
		expected := "/dev/nvme1n1"
		if resolved != expected {
			t.Errorf("Expected %q, got %q", expected, resolved)
		}
	})

	t.Run("Skip used disks", func(t *testing.T) {
		usedDisks := map[string]bool{
			"/dev/nvme1n1": true,
		}
		// Since nvme1n1 is marked used and nvme0n1 is partitioned, no matching disk should be found
		_, err := provider.ResolveDiskByModel("Dell DC NVMe", nil, usedDisks)
		if err == nil {
			t.Fatal("Expected ResolveDiskByModel to fail because no unpartitioned unused disk matches")
		}
	})

	t.Run("Case-insensitive normalization and underscores", func(t *testing.T) {
		usedDisks := make(map[string]bool)
		// Should match "Dell DC NVMe CD8 U.2 960GB"
		resolved, err := provider.ResolveDiskByModel("dell dc nvme", nil, usedDisks)
		if err != nil {
			t.Fatalf("Expected to resolve disk, got error: %v", err)
		}
		if resolved != "/dev/nvme1n1" {
			t.Errorf("Expected /dev/nvme1n1, got %q", resolved)
		}
	})

	t.Run("Wildcard match - suffix asterisk", func(t *testing.T) {
		usedDisks := make(map[string]bool)
		resolved, err := provider.ResolveDiskByModel("dell*", nil, usedDisks)
		if err != nil {
			t.Fatalf("Expected to resolve disk via wildcard, got error: %v", err)
		}
		if resolved != "/dev/nvme1n1" {
			t.Errorf("Expected /dev/nvme1n1, got %q", resolved)
		}
	})

	t.Run("Wildcard match - middle asterisk", func(t *testing.T) {
		usedDisks := make(map[string]bool)
		resolved, err := provider.ResolveDiskByModel("*cd8*", nil, usedDisks)
		if err != nil {
			t.Fatalf("Expected to resolve disk via wildcard, got error: %v", err)
		}
		if resolved != "/dev/nvme1n1" {
			t.Errorf("Expected /dev/nvme1n1, got %q", resolved)
		}
	})

	t.Run("Wildcard match - single character wildcard", func(t *testing.T) {
		usedDisks := make(map[string]bool)
		// Should match sda ("samsung_mz7wd480hagp" after normalization)
		resolved, err := provider.ResolveDiskByModel("samsung_mz7wd480hag?", nil, usedDisks)
		if err != nil {
			t.Fatalf("Expected to resolve sda, got error: %v", err)
		}
		if resolved != "/dev/sda" {
			t.Errorf("Expected /dev/sda, got %q", resolved)
		}
	})

	t.Run("Size filtering - match size", func(t *testing.T) {
		usedDisks := make(map[string]bool)
		conds := []sizeCondition{{operator: ">=", target: 850 * 1024 * 1024 * 1024}}
		resolved, err := provider.ResolveDiskByModel("Dell*", conds, usedDisks)
		if err != nil {
			t.Fatalf("Expected to resolve disk with matching size, got error: %v", err)
		}
		if resolved != "/dev/nvme1n1" {
			t.Errorf("Expected /dev/nvme1n1, got %q", resolved)
		}
	})

	t.Run("Size filtering - skip due to size", func(t *testing.T) {
		usedDisks := make(map[string]bool)
		conds := []sizeCondition{{operator: ">", target: 1000 * 1024 * 1024 * 1024}} // 1 TB, larger than 960 GB
		_, err := provider.ResolveDiskByModel("Dell*", conds, usedDisks)
		if err == nil {
			t.Fatal("Expected ResolveDiskByModel to fail because matched disk is too small")
		}
	})
}

func TestParseSizeInBytes(t *testing.T) {
	testCases := []struct {
		input string
		want  uint64
		fail  bool
	}{
		{"100", 100, false},
		{"100B", 100, false},
		{"1KB", 1024, false},
		{"1.5KB", 1536, false},
		{"10MB", 10 * 1024 * 1024, false},
		{"960GB", 960 * 1024 * 1024 * 1024, false},
		{"1.2TB", 1319413953331, false}, // 1.2 * 1024^4 = 1319413953331.2 -> 1319413953331
		{"", 0, true},
		{"GB", 0, true},
		{"10XB", 0, true},
	}

	for _, tc := range testCases {
		got, err := parseSizeInBytes(tc.input)
		if tc.fail {
			if err == nil {
				t.Errorf("parseSizeInBytes(%q) expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseSizeInBytes(%q) returned unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("parseSizeInBytes(%q) = %d; want %d", tc.input, got, tc.want)
			}
		}
	}
}

func TestParseSizeCondition(t *testing.T) {
	testCases := []struct {
		input  string
		op     string
		target uint64
		fail   bool
	}{
		{"<=10GB", "<=", 10 * 1024 * 1024 * 1024, false},
		{">=100B", ">=", 100, false},
		{"=1TB", "=", 1024 * 1024 * 1024 * 1024, false},
		{"==500MB", "==", 500 * 1024 * 1024, false},
		{"<5.5GB", "<", 5905580032, false}, // 5.5 * 1024^3
		{">10", ">", 10, false},
		{"10GB", "", 0, true},
		{"<>10GB", "", 0, true},
	}

	for _, tc := range testCases {
		got, err := parseSizeCondition(tc.input)
		if tc.fail {
			if err == nil {
				t.Errorf("parseSizeCondition(%q) expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseSizeCondition(%q) returned unexpected error: %v", tc.input, err)
			}
			if got.operator != tc.op {
				t.Errorf("parseSizeCondition(%q) operator = %q; want %q", tc.input, got.operator, tc.op)
			}
			if got.target != tc.target {
				t.Errorf("parseSizeCondition(%q) target = %d; want %d", tc.input, got.target, tc.target)
			}
		}
	}
}

func TestCreatePool_ResolveByModel_Success(t *testing.T) {
	mockProvider := &mockZFSProvider{
		ResolveDiskByModelFunc: func(model string, sizeConds []sizeCondition, usedDisks map[string]bool) (string, error) {
			if model == "Dell DC NVMe" {
				if usedDisks["/dev/nvme1n1"] {
					return "/dev/nvme2n1", nil
				}
				return "/dev/nvme1n1", nil
			}
			return "", fmt.Errorf("unknown model %q", model)
		},
	}

	config := poolConfig{
		Name:   "modelpool",
		Disks:  []diskSpec{{Model: "Dell DC NVMe"}, {Model: "Dell DC NVMe"}},
		Ashift: "12",
	}

	usedDisks := make(map[string]bool)
	var createPoolDisks []string
	mockProvider.CreatePoolFunc = func(zpoolPath string, args []string) ([]byte, error) {
		for _, arg := range args {
			if strings.HasPrefix(arg, "/dev/") {
				createPoolDisks = append(createPoolDisks, arg)
			}
		}
		return nil, nil
	}

	err := createPool(mockProvider, "/fake/zpool", config, usedDisks)
	if err != nil {
		t.Fatalf("createPool() returned an unexpected error: %v", err)
	}

	if len(createPoolDisks) != 2 {
		t.Fatalf("Expected 2 disks to be used, got %d", len(createPoolDisks))
	}
	if createPoolDisks[0] != "/dev/nvme1n1" || createPoolDisks[1] != "/dev/nvme2n1" {
		t.Errorf("Expected disks [/dev/nvme1n1, /dev/nvme2n1], got %v", createPoolDisks)
	}

	if !usedDisks["/dev/nvme1n1"] || !usedDisks["/dev/nvme2n1"] {
		t.Errorf("Expected usedDisks to be populated, got %v", usedDisks)
	}
}

func TestCreatePool_HybridOrder(t *testing.T) {
	mockProvider := &mockZFSProvider{
		ResolveDiskByModelFunc: func(model string, sizeConds []sizeCondition, usedDisks map[string]bool) (string, error) {
			if model == "Dell*" {
				return "/dev/nvme1n1", nil
			}
			return "", fmt.Errorf("unknown model pattern %q", model)
		},
	}

	config := poolConfig{
		Name:   "hybridpool",
		Disks:  []diskSpec{{Dev: "/dev/sda"}, {Model: "Dell*"}},
		Ashift: "12",
	}

	usedDisks := make(map[string]bool)
	var createPoolDisks []string
	mockProvider.CreatePoolFunc = func(zpoolPath string, args []string) ([]byte, error) {
		for _, arg := range args {
			if strings.HasPrefix(arg, "/dev/") {
				createPoolDisks = append(createPoolDisks, arg)
			}
		}
		return nil, nil
	}

	err := createPool(mockProvider, "/fake/zpool", config, usedDisks)
	if err != nil {
		t.Fatalf("createPool failed: %v", err)
	}

	// We should see both /dev/sda and the resolved model /dev/nvme1n1 in the exact order declared
	if len(createPoolDisks) != 2 || createPoolDisks[0] != "/dev/sda" || createPoolDisks[1] != "/dev/nvme1n1" {
		t.Errorf("Expected disks [/dev/sda, /dev/nvme1n1], got %v", createPoolDisks)
	}
}
