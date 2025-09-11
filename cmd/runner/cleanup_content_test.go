package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponyruntime/pony/moduleloader"
	"go.uber.org/zap"
)

// TestCleanupModuleContent tests the CleanupModuleContent function
func TestCleanupModuleContent(t *testing.T) {
	// Create a temporary directory for test
	tempDir, err := os.MkdirTemp("", "wippy-content-cleanup-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create modules directory
	modulesDir := filepath.Join(tempDir, ".wippy")
	if err := os.MkdirAll(modulesDir, 0755); err != nil {
		t.Fatalf("Failed to create modules directory: %v", err)
	}

	// Create test module with multiple versions
	modulePath := filepath.Join(modulesDir, "wippyai", "security@abc123")
	if err := os.MkdirAll(modulePath, 0755); err != nil {
		t.Fatalf("Failed to create module directory: %v", err)
	}

	// Create old version content (should be removed)
	oldVersionPath := filepath.Join(modulePath, "module-security-0.0.6")
	if err := os.MkdirAll(oldVersionPath, 0755); err != nil {
		t.Fatalf("Failed to create old version directory: %v", err)
	}
	// Create a file in old version
	if err := os.WriteFile(filepath.Join(oldVersionPath, "old.txt"), []byte("old"), 0600); err != nil {
		t.Fatalf("Failed to create old version file: %v", err)
	}

	// Create new version content (should be kept)
	newVersionPath := filepath.Join(modulePath, "module-security-0.0.7")
	if err := os.MkdirAll(newVersionPath, 0755); err != nil {
		t.Fatalf("Failed to create new version directory: %v", err)
	}
	// Create a file in new version
	if err := os.WriteFile(filepath.Join(newVersionPath, "new.txt"), []byte("new"), 0600); err != nil {
		t.Fatalf("Failed to create new version file: %v", err)
	}

	// Create lock file with new version
	lockFile := &moduleloader.LockFile{
		Directories: moduleloader.Directories{
			Modules: ".wippy",
			Src:     ".",
		},
		Modules: []moduleloader.LockedModule{
			{Name: "wippyai/security", Version: "v0.0.7", Hash: "abc123"},
		},
	}

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

	// List directory contents before cleanup
	t.Logf("Directory contents before cleanup:")
	if err := filepath.WalkDir(modulesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(modulesDir, path)
		t.Logf("  %s (dir: %v)", relPath, d.IsDir())
		return nil
	}); err != nil {
		t.Fatalf("WalkDir failed: %v", err)
	}

	// Run content cleanup
	removedContent := dm.CleanupModuleContent(context.Background(), lockFile)

	// Check results
	t.Logf("Removed content: %v", removedContent)

	// Check if old version was removed
	if _, err := os.Stat(oldVersionPath); err == nil {
		t.Errorf("Old version directory should have been removed: %s", oldVersionPath)
	} else if !os.IsNotExist(err) {
		t.Errorf("Unexpected error checking old version directory: %v", err)
	} else {
		t.Logf("✓ Old version directory was successfully removed: %s", oldVersionPath)
	}

	// Check if new version still exists
	if _, err := os.Stat(newVersionPath); os.IsNotExist(err) {
		t.Errorf("New version directory should still exist: %s", newVersionPath)
	} else if err != nil {
		t.Errorf("Unexpected error checking new version directory: %v", err)
	} else {
		t.Logf("✓ New version directory still exists: %s", newVersionPath)
	}

	// Check that we removed exactly one item
	switch {
	case len(removedContent) != 1:
		t.Errorf("Expected 1 content item to be removed, got %d: %v", len(removedContent), removedContent)
	case removedContent[0] != "module-security-0.0.6":
		t.Errorf("Expected 'module-security-0.0.6' to be removed, got: %s", removedContent[0])
	default:
		t.Logf("✓ Correct content was removed: %s", removedContent[0])
	}

	// List directory contents after cleanup
	t.Logf("Directory contents after cleanup:")
	if err := filepath.WalkDir(modulesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(modulesDir, path)
		t.Logf("  %s (dir: %v)", relPath, d.IsDir())
		return nil
	}); err != nil {
		t.Fatalf("WalkDir failed: %v", err)
	}
}

// TestCleanupAllUnusedModules tests the comprehensive cleanup function
func TestCleanupAllUnusedModules(t *testing.T) {
	// Create a temporary directory for test
	tempDir, err := os.MkdirTemp("", "wippy-comprehensive-cleanup-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create modules directory
	modulesDir := filepath.Join(tempDir, ".wippy")
	if err := os.MkdirAll(modulesDir, 0755); err != nil {
		t.Fatalf("Failed to create modules directory: %v", err)
	}

	// Create used module with multiple versions
	usedModulePath := filepath.Join(modulesDir, "wippyai", "security@abc123")
	if err := os.MkdirAll(usedModulePath, 0755); err != nil {
		t.Fatalf("Failed to create used module directory: %v", err)
	}

	// Create old version content (should be removed)
	oldVersionPath := filepath.Join(usedModulePath, "module-security-0.0.6")
	if err := os.MkdirAll(oldVersionPath, 0755); err != nil {
		t.Fatalf("Failed to create old version directory: %v", err)
	}

	// Create new version content (should be kept)
	newVersionPath := filepath.Join(usedModulePath, "module-security-0.0.7")
	if err := os.MkdirAll(newVersionPath, 0755); err != nil {
		t.Fatalf("Failed to create new version directory: %v", err)
	}

	// Create unused module (should be removed entirely)
	unusedModulePath := filepath.Join(modulesDir, "wippyai", "unused@def456")
	if err := os.MkdirAll(unusedModulePath, 0755); err != nil {
		t.Fatalf("Failed to create unused module directory: %v", err)
	}

	// Create lock file with only the used module
	lockFile := &moduleloader.LockFile{
		Directories: moduleloader.Directories{
			Modules: ".wippy",
			Src:     ".",
		},
		Modules: []moduleloader.LockedModule{
			{Name: "wippyai/security", Version: "v0.0.7", Hash: "abc123"},
		},
	}

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

	// List directory contents before cleanup
	t.Logf("Directory contents before cleanup:")
	if err := filepath.WalkDir(modulesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(modulesDir, path)
		t.Logf("  %s (dir: %v)", relPath, d.IsDir())
		return nil
	}); err != nil {
		t.Fatalf("WalkDir failed: %v", err)
	}

	// Run comprehensive cleanup
	stats := NewModuleOperationStats()
	err = dm.CleanupAllUnusedModules(context.Background(), modulesDir, lockFile, stats)
	if err != nil {
		t.Fatalf("CleanupAllUnusedModules failed: %v", err)
	}

	// Also clean up module content (old versions)
	removedContent := dm.CleanupModuleContent(context.Background(), lockFile)
	t.Logf("Removed content: %v", removedContent)

	// Check results
	t.Logf("Cleanup stats: %+v", stats)

	// Check if unused module was removed
	if _, err := os.Stat(unusedModulePath); err == nil {
		t.Errorf("Unused module directory should have been removed: %s", unusedModulePath)
	} else if !os.IsNotExist(err) {
		t.Errorf("Unexpected error checking unused module directory: %v", err)
	} else {
		t.Logf("✓ Unused module directory was successfully removed: %s", unusedModulePath)
	}

	// Check if used module still exists
	if _, err := os.Stat(usedModulePath); os.IsNotExist(err) {
		t.Errorf("Used module directory should still exist: %s", usedModulePath)
	} else if err != nil {
		t.Errorf("Unexpected error checking used module directory: %v", err)
	} else {
		t.Logf("✓ Used module directory still exists: %s", usedModulePath)
	}

	// Check if old version content was removed
	if _, err := os.Stat(oldVersionPath); err == nil {
		t.Errorf("Old version content should have been removed: %s", oldVersionPath)
	} else if !os.IsNotExist(err) {
		t.Errorf("Unexpected error checking old version content: %v", err)
	} else {
		t.Logf("✓ Old version content was successfully removed: %s", oldVersionPath)
	}

	// Check if new version content still exists
	if _, err := os.Stat(newVersionPath); os.IsNotExist(err) {
		t.Errorf("New version content should still exist: %s", newVersionPath)
	} else if err != nil {
		t.Errorf("Unexpected error checking new version content: %v", err)
	} else {
		t.Logf("✓ New version content still exists: %s", newVersionPath)
	}

	// Check stats
	if stats.Removed != 1 {
		t.Errorf("Expected 1 module to be removed, got %d", stats.Removed)
	} else {
		t.Logf("✓ Correct number of modules removed: %d", stats.Removed)
	}

	// Check that content was removed
	switch {
	case len(removedContent) != 1:
		t.Errorf("Expected 1 content item to be removed, got %d: %v", len(removedContent), removedContent)
	case removedContent[0] != "module-security-0.0.6":
		t.Errorf("Expected 'module-security-0.0.6' to be removed, got: %s", removedContent[0])
	default:
		t.Logf("✓ Correct content was removed: %s", removedContent[0])
	}

	// List directory contents after cleanup
	t.Logf("Directory contents after cleanup:")
	if err := filepath.WalkDir(modulesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(modulesDir, path)
		t.Logf("  %s (dir: %v)", relPath, d.IsDir())
		return nil
	}); err != nil {
		t.Fatalf("WalkDir failed: %v", err)
	}
}
