package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponyruntime/pony/moduleloader"
	"go.uber.org/zap"
)

// TestCleanupUnusedModules is a matrix test that covers various scenarios
func TestCleanupUnusedModules(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(t *testing.T, tempDir string) *moduleloader.LockFile
		expectedRemoved map[string]string // moduleName -> relativePath
		expectError     bool
		description     string
	}{
		{
			name: "no_modules_directory",
			setup: func(_ *testing.T, _ string) *moduleloader.LockFile {
				// Don't create modules directory
				return createTestLockFile([]moduleloader.LockedModule{
					{Name: "org1/module1", Version: "1.0.0", Hash: "abc123"},
				}, ".wippy")
			},
			expectedRemoved: map[string]string{},
			expectError:     false,
			description:     "Should handle missing modules directory gracefully",
		},
		{
			name: "empty_modules_directory",
			setup: func(t *testing.T, tempDir string) *moduleloader.LockFile {
				// Create empty modules directory
				modulesDir := filepath.Join(tempDir, ".wippy")
				if err := os.MkdirAll(modulesDir, 0755); err != nil {
					t.Fatalf("Failed to create modules directory: %v", err)
				}
				return createTestLockFile([]moduleloader.LockedModule{
					{Name: "org1/module1", Version: "1.0.0", Hash: "abc123"},
				}, ".wippy")
			},
			expectedRemoved: map[string]string{},
			expectError:     false,
			description:     "Should handle empty modules directory",
		},
		{
			name: "remove_unused_modules",
			setup: func(t *testing.T, tempDir string) *moduleloader.LockFile {
				// Create modules directory with unused modules
				modulesDir := filepath.Join(tempDir, ".wippy")
				if err := os.MkdirAll(modulesDir, 0755); err != nil {
					t.Fatalf("Failed to create modules directory: %v", err)
				}

				// Create unused module directories (using real structure)
				createRealModuleDir(t, modulesDir, "unused-actor", "")
				createRealModuleDir(t, modulesDir, "unused-agents", "")
				createRealModuleDir(t, modulesDir, "unused-llm", "")

				// Create used module directory (should not be removed)
				createRealModuleDir(t, modulesDir, "actor", "")

				return createTestLockFile([]moduleloader.LockedModule{
					{Name: "wippyai/actor", Version: "1.0.0"},
				}, ".wippy")
			},
			expectedRemoved: map[string]string{
				"wippyai/unused-actor":  "wippyai/unused-actor/module-unused-actor",
				"wippyai/unused-agents": "wippyai/unused-agents/module-unused-agents",
				"wippyai/unused-llm":    "wippyai/unused-llm/module-unused-llm",
			},
			expectError: false,
			description: "Should remove unused modules and keep used ones",
		},
		{
			name: "keep_modules_with_versions",
			setup: func(t *testing.T, tempDir string) *moduleloader.LockFile {
				// Create modules directory with modules that have versions
				modulesDir := filepath.Join(tempDir, ".wippy")
				if err := os.MkdirAll(modulesDir, 0755); err != nil {
					t.Fatalf("Failed to create modules directory: %v", err)
				}

				// Create module directories with versions (using real structure)
				createRealModuleDir(t, modulesDir, "actor", "abc123")
				createRealModuleDir(t, modulesDir, "agents", "def456")
				createRealModuleDir(t, modulesDir, "llm", "ghi789")

				// Create unused module
				createRealModuleDir(t, modulesDir, "unused-migration", "jkl012")

				return createTestLockFile([]moduleloader.LockedModule{
					{Name: "wippyai/actor", Version: "1.0.0", Hash: "abc123"},
					{Name: "wippyai/agents", Version: "2.0.0", Hash: "def456"},
					{Name: "wippyai/llm", Version: "3.0.0", Hash: "ghi789"},
				}, ".wippy")
			},
			expectedRemoved: map[string]string{
				"wippyai/unused-migration": "wippyai/unused-migration@jkl012/module-unused-migration-jkl012",
			},
			expectError: false,
			description: "Should keep modules with versions and remove unused ones",
		},
		{
			name: "keep_modules_without_versions",
			setup: func(t *testing.T, tempDir string) *moduleloader.LockFile {
				// Create modules directory with modules without versions
				modulesDir := filepath.Join(tempDir, ".wippy")
				if err := os.MkdirAll(modulesDir, 0755); err != nil {
					t.Fatalf("Failed to create modules directory: %v", err)
				}

				// Create module directories without versions (using real structure)
				createRealModuleDir(t, modulesDir, "actor", "")
				createRealModuleDir(t, modulesDir, "agents", "")
				createRealModuleDir(t, modulesDir, "llm", "")

				// Create unused module
				createRealModuleDir(t, modulesDir, "unused-test", "")

				return createTestLockFile([]moduleloader.LockedModule{
					{Name: "wippyai/actor", Version: "1.0.0"},
					{Name: "wippyai/agents", Version: "2.0.0"},
					{Name: "wippyai/llm", Version: "3.0.0"},
				}, ".wippy")
			},
			expectedRemoved: map[string]string{
				"wippyai/unused-test": "wippyai/unused-test/module-unused-test",
			},
			expectError: false,
			description: "Should keep modules without versions and remove unused ones",
		},
		{
			name: "handle_replacements",
			setup: func(t *testing.T, tempDir string) *moduleloader.LockFile {
				// Create modules directory
				modulesDir := filepath.Join(tempDir, ".wippy")
				if err := os.MkdirAll(modulesDir, 0755); err != nil {
					t.Fatalf("Failed to create modules directory: %v", err)
				}

				// Create replacement directory outside modules directory
				replacementDir := filepath.Join(tempDir, "custom", "wippyai", "actor")
				if err := os.MkdirAll(replacementDir, 0755); err != nil {
					t.Fatalf("Failed to create replacement directory: %v", err)
				}

				// Create unused module in modules directory
				createRealModuleDir(t, modulesDir, "unused-migration", "abc123")

				// Create lock file with replacement
				lockFile := createTestLockFile([]moduleloader.LockedModule{
					{Name: "wippyai/actor", Version: "1.0.0"},
				}, ".wippy")
				lockFile.Replacements = []moduleloader.Replacement{
					{From: "wippyai/actor", To: "custom/wippyai/actor"},
				}

				return lockFile
			},
			expectedRemoved: map[string]string{
				"wippyai/unused-migration": "wippyai/unused-migration@abc123/module-unused-migration-abc123",
			},
			expectError: false,
			description: "Should handle replacements and not remove replacement directories",
		},
		{
			name: "custom_modules_directory",
			setup: func(t *testing.T, tempDir string) *moduleloader.LockFile {
				// Create custom modules directory
				modulesDir := filepath.Join(tempDir, "custom_modules")
				if err := os.MkdirAll(modulesDir, 0755); err != nil {
					t.Fatalf("Failed to create modules directory: %v", err)
				}

				// Create unused module
				createRealModuleDir(t, modulesDir, "unused-specs", "abc123")

				// Create used module
				createRealModuleDir(t, modulesDir, "actor", "def456")

				return createTestLockFile([]moduleloader.LockedModule{
					{Name: "wippyai/actor", Version: "1.0.0", Hash: "def456"},
				}, "custom_modules")
			},
			expectedRemoved: map[string]string{
				"wippyai/unused-specs": "wippyai/unused-specs@abc123/module-unused-specs-abc123",
			},
			expectError: false,
			description: "Should work with custom modules directory",
		},
		{
			name: "invalid_module_names",
			setup: func(t *testing.T, tempDir string) *moduleloader.LockFile {
				// Create modules directory
				modulesDir := filepath.Join(tempDir, ".wippy")
				if err := os.MkdirAll(modulesDir, 0755); err != nil {
					t.Fatalf("Failed to create modules directory: %v", err)
				}

				// Create module with invalid name in lock file
				// This should be handled gracefully
				return createTestLockFile([]moduleloader.LockedModule{
					{Name: "invalid-module-name", Version: "1.0.0"}, // Invalid format
				}, ".wippy")
			},
			expectedRemoved: map[string]string{},
			expectError:     false,
			description:     "Should handle invalid module names gracefully",
		},
		{
			name: "nested_directories",
			setup: func(t *testing.T, tempDir string) *moduleloader.LockFile {
				// Create modules directory
				modulesDir := filepath.Join(tempDir, ".wippy")
				if err := os.MkdirAll(modulesDir, 0755); err != nil {
					t.Fatalf("Failed to create modules directory: %v", err)
				}

				// Create used module with nested structure (like real wippy modules)
				createRealModuleDir(t, modulesDir, "actor", "abc123")

				return createTestLockFile([]moduleloader.LockedModule{
					{Name: "wippyai/actor", Version: "1.0.0", Hash: "abc123"},
				}, ".wippy")
			},
			expectedRemoved: map[string]string{},
			expectError:     false,
			description:     "Should handle nested directories correctly",
		},
		{
			name: "real_wippy_structure",
			setup: func(t *testing.T, tempDir string) *moduleloader.LockFile {
				// Create modules directory
				modulesDir := filepath.Join(tempDir, ".wippy")
				if err := os.MkdirAll(modulesDir, 0755); err != nil {
					t.Fatalf("Failed to create modules directory: %v", err)
				}

				// Create modules that should be kept (based on real wippy structure)
				createRealModuleDir(t, modulesDir, "actor", "")
				createRealModuleDir(t, modulesDir, "agents", "")
				createRealModuleDir(t, modulesDir, "llm", "")

				// Create unused modules that should be removed
				createRealModuleDir(t, modulesDir, "unused-btea", "")
				createRealModuleDir(t, modulesDir, "unused-migration", "")

				return createTestLockFile([]moduleloader.LockedModule{
					{Name: "wippyai/actor", Version: "1.0.0"},
					{Name: "wippyai/agents", Version: "1.0.0"},
					{Name: "wippyai/llm", Version: "1.0.0"},
				}, ".wippy")
			},
			expectedRemoved: map[string]string{
				"wippyai/unused-btea":      "wippyai/unused-btea/module-unused-btea",
				"wippyai/unused-migration": "wippyai/unused-migration/module-unused-migration",
			},
			expectError: false,
			description: "Should work with real wippy module structure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir, err := os.MkdirTemp("", "wippy-cleanup-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			// Setup test scenario
			lockFile := tt.setup(t, tempDir)

			// Create dependency manager
			config := &Config{
				FolderPath: tempDir,
				LockFile:   "wippy.lock",
			}

			logger, err := zap.NewDevelopment()
			if err != nil {
				t.Fatalf("Failed to create logger: %v", err)
			}

			dm := NewDependencyManager(config, logger)

			// Run cleanup
			removedModules, err := dm.CleanupUnusedModules(context.Background(), lockFile)

			// Check error expectation
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Check removed modules
			if !stringMapsEqual(removedModules, tt.expectedRemoved) {
				t.Errorf("Expected removed modules %v, got %v", tt.expectedRemoved, removedModules)
			}

			t.Logf("Test case: %s - %s", tt.name, tt.description)
		})
	}
}

// TestExtractVersionFromPath tests the version extraction function
func TestExtractVersionFromPath(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	dm := &DependencyManager{logger: logger}

	tests := []struct {
		path     string
		expected string
	}{
		{
			path:     "wippy/security@01978c92-7d02-7b4a-95df-55b57cfe80b7/module-security-0.0.7",
			expected: "0.0.7",
		},
		{
			path:     "wippyai/actor@abc123/module-actor-1.2.3",
			expected: "1.2.3",
		},
		{
			path:     "wippyai/test@def456/module-test-2.0.0-beta.1",
			expected: "2.0.0-beta.1",
		},
		{
			path:     "wippyai/simple/module-simple-1.0.0",
			expected: "1.0.0",
		},
		{
			path:     "wippyai/invalid/module-invalid",
			expected: "unknown",
		},
		{
			path:     "wippyai/short/module-short-1",
			expected: "unknown",
		},
		{
			path:     "wippyai/empty",
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := dm.extractVersionFromPath(tt.path)
			if result != tt.expected {
				t.Errorf("extractVersionFromPath(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

// TestVersionPrefix tests that versions get the "v" prefix when added to stats
func TestVersionPrefix(t *testing.T) {
	stats := &ModuleOperationStats{}

	// Test with a valid version
	version := "0.0.7"
	if version != "unknown" {
		version = "v" + version
	}
	stats.AddRemoved("wippyai/test", version)

	// Check that the version has the "v" prefix
	if len(stats.Operations) != 1 {
		t.Fatalf("Expected 1 operation, got %d", len(stats.Operations))
	}

	if stats.Operations[0].Version != "v0.0.7" {
		t.Errorf("Expected version 'v0.0.7', got '%s'", stats.Operations[0].Version)
	}

	// Test with unknown version
	stats2 := &ModuleOperationStats{}
	unknownVersion := "unknown"
	if unknownVersion != "unknown" {
		unknownVersion = "v" + unknownVersion
	}
	stats2.AddRemoved("wippyai/test", unknownVersion)

	if stats2.Operations[0].Version != "unknown" {
		t.Errorf("Expected version 'unknown', got '%s'", stats2.Operations[0].Version)
	}
}

// TestInstallAfterCleanup tests that modules are properly installed after cleanup
func TestInstallAfterCleanup(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "wippy-install-cleanup-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create modules directory
	modulesDir := filepath.Join(tempDir, ".wippy")
	if err := os.MkdirAll(modulesDir, 0755); err != nil {
		t.Fatalf("Failed to create modules directory: %v", err)
	}

	// Create an old version of a module (simulating v0.0.6)
	oldModuleDir := filepath.Join(modulesDir, "wippy", "security@old-hash")
	if err := os.MkdirAll(oldModuleDir, 0755); err != nil {
		t.Fatalf("Failed to create old module directory: %v", err)
	}

	// Create a lock file with the new version (v0.0.7)
	lockFile := &moduleloader.LockFile{
		Directories: moduleloader.Directories{
			Modules: ".wippy",
			Src:     ".",
		},
		Modules: []moduleloader.LockedModule{
			{
				Name:    "wippy/security",
				Version: "v0.0.7",
				Hash:    "new-hash-123",
			},
		},
	}

	// Create config and logger
	config := &Config{
		FolderPath: tempDir,
		LockFile:   "wippy.lock",
	}
	logger, _ := zap.NewDevelopment()
	dm := NewDependencyManager(config, logger)

	// Test that the old module is detected as installed
	wasInstalled, oldVersion := dm.isModuleInstalledFromLockFile(lockFile.Modules[0], lockFile)
	if wasInstalled {
		t.Logf("Old module detected as installed with version: %s", oldVersion)
	}

	// Test cleanup - should remove the old module
	stats := NewModuleOperationStats(false) // Test with non-verbose mode
	err = dm.CleanupAllUnusedModules(context.Background(), ".", lockFile, stats)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Check that old module directory was removed
	if _, err := os.Stat(oldModuleDir); !os.IsNotExist(err) {
		t.Errorf("Old module directory should have been removed: %s", oldModuleDir)
	}

	// Check that the module is no longer detected as installed
	wasInstalledAfter, _ := dm.isModuleInstalledFromLockFile(lockFile.Modules[0], lockFile)
	if wasInstalledAfter {
		t.Errorf("Module should not be detected as installed after cleanup")
	}

	t.Logf("Cleanup completed successfully. Removed modules: %d", stats.Removed)
}

// TestExtractModuleNameFromPath tests the helper function
func TestExtractModuleNameFromPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"org1/module1", "org1/module1"},
		{"org1/module1@1.0.0", "org1/module1"},
		{"org1/module1@abc123", "org1/module1"},
		{"org1/module1@version-with-dashes", "org1/module1"},
		{"org1", ""},                      // Invalid format
		{"", ""},                          // Empty string
		{"org1/module1@", "org1/module1"}, // Edge case with empty version
	}

	config := &Config{}
	logger, _ := zap.NewDevelopment()
	dm := NewDependencyManager(config, logger)

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := dm.extractModuleNameFromPath(tt.input)
			if result != tt.expected {
				t.Errorf("extractModuleNameFromPath(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

// Helper functions

// createTestLockFile creates a test lock file with the given modules
func createTestLockFile(modules []moduleloader.LockedModule, modulesDir string) *moduleloader.LockFile {
	return &moduleloader.LockFile{
		Directories: moduleloader.Directories{
			Modules: modulesDir,
			Src:     ".",
		},
		Modules:      modules,
		Replacements: []moduleloader.Replacement{},
	}
}

// createRealModuleDir creates a module directory structure using real wippy format
func createRealModuleDir(t *testing.T, baseDir, module, version string) {
	var dirName string
	if version != "" {
		dirName = module + "@" + version
	} else {
		dirName = module
	}

	// Create the main module directory (with hash)
	moduleDir := filepath.Join(baseDir, "wippyai", dirName)
	if err := os.MkdirAll(moduleDir, 0755); err != nil {
		t.Fatalf("Failed to create module directory %s: %v", moduleDir, err)
	}

	// Create the actual module folder with version in the name
	var moduleFolderName string
	if version != "" {
		moduleFolderName = "module-" + module + "-" + version
	} else {
		moduleFolderName = "module-" + module
	}

	modulePath := filepath.Join(moduleDir, moduleFolderName)
	if err := os.MkdirAll(modulePath, 0755); err != nil {
		t.Fatalf("Failed to create module folder %s: %v", modulePath, err)
	}

	// Create _index.yaml file (required for wippy modules)
	indexFile := filepath.Join(modulePath, "_index.yaml")
	indexContent := `name: wippyai/` + module + `
version: 1.0.0
description: Test module for cleanup tests
`
	if err := os.WriteFile(indexFile, []byte(indexContent), 0600); err != nil {
		t.Fatalf("Failed to create _index.yaml file: %v", err)
	}

	// Create main module file
	moduleFile := filepath.Join(modulePath, module+".lua")
	moduleContent := `-- Test module for cleanup tests
local M = {}

function M.test()
    return "test"
end

return M
`
	if err := os.WriteFile(moduleFile, []byte(moduleContent), 0600); err != nil {
		t.Fatalf("Failed to create module file: %v", err)
	}

	// Create test file (optional but common in wippy modules)
	testFile := filepath.Join(modulePath, module+"_test.lua")
	testContent := `-- Test file for cleanup tests
local M = require("` + module + `")

function test_basic()
    assert(M.test() == "test")
end
`
	if err := os.WriteFile(testFile, []byte(testContent), 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
}

// stringMapsEqual compares two maps of string to string
func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if b[k] != v {
			return false
		}
	}

	return true
}

// TestIsVersionDowngrade tests the semver-based version comparison
func TestIsVersionDowngrade(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	dm := &DependencyManager{logger: logger}

	tests := []struct {
		name        string
		oldVersion  string
		newVersion  string
		expected    bool
		description string
	}{
		{
			name:        "major_downgrade",
			oldVersion:  "2.0.0",
			newVersion:  "1.0.0",
			expected:    true,
			description: "Major version downgrade should be detected",
		},
		{
			name:        "minor_downgrade",
			oldVersion:  "1.2.0",
			newVersion:  "1.1.0",
			expected:    true,
			description: "Minor version downgrade should be detected",
		},
		{
			name:        "patch_downgrade",
			oldVersion:  "1.0.2",
			newVersion:  "1.0.1",
			expected:    true,
			description: "Patch version downgrade should be detected",
		},
		{
			name:        "major_upgrade",
			oldVersion:  "1.0.0",
			newVersion:  "2.0.0",
			expected:    false,
			description: "Major version upgrade should not be detected as downgrade",
		},
		{
			name:        "minor_upgrade",
			oldVersion:  "1.0.0",
			newVersion:  "1.1.0",
			expected:    false,
			description: "Minor version upgrade should not be detected as downgrade",
		},
		{
			name:        "patch_upgrade",
			oldVersion:  "1.0.0",
			newVersion:  "1.0.1",
			expected:    false,
			description: "Patch version upgrade should not be detected as downgrade",
		},
		{
			name:        "same_version",
			oldVersion:  "1.0.0",
			newVersion:  "1.0.0",
			expected:    false,
			description: "Same version should not be detected as downgrade",
		},
		{
			name:        "prerelease_downgrade",
			oldVersion:  "1.0.0",
			newVersion:  "1.0.0-beta",
			expected:    true,
			description: "Prerelease downgrade should be detected",
		},
		{
			name:        "prerelease_upgrade",
			oldVersion:  "1.0.0-beta",
			newVersion:  "1.0.0",
			expected:    false,
			description: "Prerelease upgrade should not be detected as downgrade",
		},
		{
			name:        "invalid_old_version",
			oldVersion:  "invalid",
			newVersion:  "1.0.0",
			expected:    false,
			description: "Invalid old version should return false",
		},
		{
			name:        "invalid_new_version",
			oldVersion:  "1.0.0",
			newVersion:  "invalid",
			expected:    false,
			description: "Invalid new version should return false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dm.isVersionDowngrade(tt.oldVersion, tt.newVersion)
			if result != tt.expected {
				t.Errorf("isVersionDowngrade(%s, %s) = %v, expected %v. %s",
					tt.oldVersion, tt.newVersion, result, tt.expected, tt.description)
			}
		})
	}
}
