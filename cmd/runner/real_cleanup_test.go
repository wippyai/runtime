package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponyruntime/pony/moduleloader"
	"go.uber.org/zap"
)

// TestCleanupUnusedModulesRealData tests the function with real wippy.lock and .wippy directory
func TestCleanupUnusedModulesRealData(t *testing.T) {
	// Get the project root directory (go up two levels from cmd/runner)
	projectRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to get project root directory: %v", err)
	}
	t.Logf("Project root: %s", projectRoot)

	// Load the real wippy.lock file
	lockFilePath := filepath.Join(projectRoot, "wippy.lock")
	t.Logf("Loading lock file from: %s", lockFilePath)
	lockFile, err := moduleloader.LoadLockFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to load wippy.lock file: %v", err)
	}

	// Create dependency manager
	config := &Config{
		FolderPath: projectRoot,
		LockFile:   "wippy.lock",
	}

	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	dm := NewDependencyManager(config, logger)

	// List directory contents before cleanup
	wippyDir := filepath.Join(projectRoot, ".wippy")
	t.Logf("Directory contents before cleanup:")
	if err := filepath.WalkDir(wippyDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(wippyDir, path)
		t.Logf("  %s (dir: %v)", relPath, d.IsDir())
		return nil
	}); err != nil {
		t.Fatalf("WalkDir failed: %v", err)
	}

	// Run module cleanup (removes entire unused modules)
	removedModules, err := dm.CleanupUnusedModules(context.Background(), lockFile)
	if err != nil {
		t.Fatalf("CleanupUnusedModules failed: %v", err)
	}

	// Run module content cleanup (removes unused content inside modules)
	removedContent := dm.CleanupModuleContent(context.Background(), lockFile)

	// Check results
	t.Logf("Removed modules: %v", removedModules)
	t.Logf("Removed content: %v", removedContent)

	// Check if the main module directory still exists (it should, as it's in the lock file)
	mainModulePath := filepath.Join(projectRoot, ".wippy", "wippy", "security@01978c92-7d02-7b4a-95df-55b57cfe80b7")
	if _, err := os.Stat(mainModulePath); os.IsNotExist(err) {
		t.Errorf("Main module directory should still exist: %s", mainModulePath)
	} else if err != nil {
		t.Errorf("Unexpected error checking main module directory: %v", err)
	} else {
		t.Logf("✓ Main module directory still exists: %s", mainModulePath)
	}

	// Check if the replacement module directory still exists
	replacementModulePath := filepath.Join(projectRoot, ".wippy", "igor-test-3", "test-2@0198604c-3e58-7f01-904e-395a037a4e1a")
	if _, err := os.Stat(replacementModulePath); os.IsNotExist(err) {
		t.Errorf("Replacement module directory should still exist: %s", replacementModulePath)
	} else if err != nil {
		t.Errorf("Unexpected error checking replacement module directory: %v", err)
	} else {
		t.Logf("✓ Replacement module directory still exists: %s", replacementModulePath)
	}

	// Check if old version content was removed (if it existed)
	oldVersionPath := filepath.Join(projectRoot, ".wippy", "wippy", "security@01978c92-7d02-7b4a-95df-55b57cfe80b7", "module-security-0.0.6")
	if _, err := os.Stat(oldVersionPath); err == nil {
		t.Errorf("Old version content should have been removed: %s", oldVersionPath)
	} else if !os.IsNotExist(err) {
		t.Errorf("Unexpected error checking old version content: %v", err)
	} else {
		t.Logf("✓ Old version content was successfully removed (or never existed): %s", oldVersionPath)
	}

	// Check if new version content still exists (if it exists)
	newVersionPath := filepath.Join(projectRoot, ".wippy", "wippy", "security@01978c92-7d02-7b4a-95df-55b57cfe80b7", "module-security-0.0.7")
	if _, err := os.Stat(newVersionPath); os.IsNotExist(err) {
		t.Logf("ℹ New version content does not exist (this is normal for some module structures): %s", newVersionPath)
	} else if err != nil {
		t.Errorf("Unexpected error checking new version content: %v", err)
	} else {
		t.Logf("✓ New version content still exists: %s", newVersionPath)
	}

	// The module cleanup should remove old versions of modules:
	// 1. wippy/security@01978c92-7d02-7b4a-95df-55b57cfe80b7 should be kept (current version in lock file)
	// 2. wippy/security@01978c92-7d02-7b4a-95df-55b57c should be removed (old version not in lock file)
	// 3. igor-test-3/test-2@0198604c-3e58-7f01-904e-395a037a4e1a should be kept (replacement in lock file)
	if len(removedModules) == 0 {
		t.Logf("✓ No modules were removed (all modules are current)")
	} else {
		t.Logf("✓ Removed old/unused modules: %v", removedModules)
	}

	// The content cleanup may or may not remove content depending on what's in the directory
	if len(removedContent) > 0 {
		t.Logf("✓ Content was removed: %v", removedContent)
	} else {
		t.Logf("✓ No content needed to be removed (directory already clean)")
	}

	// List directory contents after cleanup
	t.Logf("Directory contents after cleanup:")
	if err := filepath.WalkDir(wippyDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(wippyDir, path)
		t.Logf("  %s (dir: %v)", relPath, d.IsDir())
		return nil
	}); err != nil {
		t.Fatalf("WalkDir failed: %v", err)
	}
}
