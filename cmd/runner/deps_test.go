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
		expectedRemoved []string
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
			expectedRemoved: []string{},
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
			expectedRemoved: []string{},
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
			expectedRemoved: []string{"wippyai/unused-actor", "wippyai/unused-agents", "wippyai/unused-llm"},
			expectError:     false,
			description:     "Should remove unused modules and keep used ones",
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
			expectedRemoved: []string{"wippyai/unused-migration"},
			expectError:     false,
			description:     "Should keep modules with versions and remove unused ones",
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
			expectedRemoved: []string{"wippyai/unused-test"},
			expectError:     false,
			description:     "Should keep modules without versions and remove unused ones",
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
			expectedRemoved: []string{"wippyai/unused-migration"},
			expectError:     false,
			description:     "Should handle replacements and not remove replacement directories",
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
			expectedRemoved: []string{"wippyai/unused-specs"},
			expectError:     false,
			description:     "Should work with custom modules directory",
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
			expectedRemoved: []string{},
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
			expectedRemoved: []string{},
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
			expectedRemoved: []string{"wippyai/unused-btea", "wippyai/unused-migration"},
			expectError:     false,
			description:     "Should work with real wippy module structure",
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
			if !stringSlicesEqual(removedModules, tt.expectedRemoved) {
				t.Errorf("Expected removed modules %v, got %v", tt.expectedRemoved, removedModules)
			}

			t.Logf("Test case: %s - %s", tt.name, tt.description)
		})
	}
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

	modulePath := filepath.Join(baseDir, "wippyai", dirName)
	if err := os.MkdirAll(modulePath, 0755); err != nil {
		t.Fatalf("Failed to create module directory %s: %v", modulePath, err)
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

// stringSlicesEqual compares two string slices for equality
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	// Create maps for comparison
	mapA := make(map[string]int)
	mapB := make(map[string]int)

	for _, s := range a {
		mapA[s]++
	}
	for _, s := range b {
		mapB[s]++
	}

	for k, v := range mapA {
		if mapB[k] != v {
			return false
		}
	}

	return true
}
