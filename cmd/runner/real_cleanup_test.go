package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponyruntime/pony/cmd/runner/app"

	"github.com/ponyruntime/pony/deps"
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
	lockFile, err := deps.LoadLockFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to load wippy.lock file: %v", err)
	}

	// Ensure required module directories exist for testing
	ensureModuleDirectoriesExist(t, projectRoot, lockFile)

	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	dm := app.NewDependencyManager(projectRoot, "wippy.lock", logger)

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

	// Check if all modules from lock file still exist (they should, as they're in the lock file)
	for _, module := range lockFile.Modules {
		// Skip replacement modules as they are handled separately
		isReplacement := false
		for _, replacement := range lockFile.Replacements {
			if replacement.From == module.Name {
				isReplacement = true
				break
			}
		}
		if isReplacement {
			continue
		}

		// Parse the module name to get organization and module parts
		name, err := deps.ParseName(module.Name)
		if err != nil {
			t.Errorf("Failed to parse module name %s: %v", module.Name, err)
			continue
		}

		// Build module path dynamically based on organization, module name and hash
		moduleDirName := name.Organization + "/" + name.Module + "@" + module.Hash
		modulePath := filepath.Join(projectRoot, ".wippy", moduleDirName)

		if _, err := os.Stat(modulePath); os.IsNotExist(err) {
			t.Errorf("Module directory should still exist: %s", modulePath)
		} else if err != nil {
			t.Errorf("Unexpected error checking module directory %s: %v", modulePath, err)
		} else {
			t.Logf("✓ Module directory still exists: %s", modulePath)
		}
	}

	// Check if all replacement module directories still exist
	for _, replacement := range lockFile.Replacements {
		// Find the corresponding module to get its hash
		var moduleHash string
		for _, module := range lockFile.Modules {
			if module.Name == replacement.From {
				moduleHash = module.Hash
				break
			}
		}

		if moduleHash != "" {
			// Parse the replacement name to get organization and module parts
			name, err := deps.ParseName(replacement.From)
			if err != nil {
				t.Errorf("Failed to parse replacement name %s: %v", replacement.From, err)
				continue
			}

			// Build replacement path dynamically based on organization, module name and hash
			replacementDirName := name.Organization + "/" + name.Module + "@" + moduleHash
			replacementPath := filepath.Join(projectRoot, ".wippy", replacementDirName)

			if _, err := os.Stat(replacementPath); os.IsNotExist(err) {
				t.Errorf("Replacement module directory should still exist: %s", replacementPath)
			} else if err != nil {
				t.Errorf("Unexpected error checking replacement module directory %s: %v", replacementPath, err)
			} else {
				t.Logf("✓ Replacement module directory still exists: %s", replacementPath)
			}
		}
	}

	// The module cleanup should remove old versions of modules:
	// 1. All modules listed in lock file should be kept (current versions)
	// 2. Old versions not in lock file should be removed
	// 3. Replacement modules should be kept (they are handled separately)
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

// ensureModuleDirectoriesExist creates the required module directories for testing
// This makes the test more robust and independent of existing directory structure
func ensureModuleDirectoriesExist(t *testing.T, projectRoot string, lockFile *deps.LockFile) {
	wippyDir := filepath.Join(projectRoot, ".wippy")

	// Ensure .wippy directory exists
	if err := os.MkdirAll(wippyDir, 0755); err != nil {
		t.Fatalf("Failed to create .wippy directory: %v", err)
	}

	// Create directories for all modules in lock file
	for _, module := range lockFile.Modules {
		// Parse the module name to get organization and module parts
		name, err := deps.ParseName(module.Name)
		if err != nil {
			t.Fatalf("Failed to parse module name %s: %v", module.Name, err)
		}

		// Create the directory structure: Organization/Module@Hash
		moduleDirName := name.Organization + "/" + name.Module + "@" + module.Hash
		modulePath := filepath.Join(wippyDir, moduleDirName)

		if err := os.MkdirAll(modulePath, 0755); err != nil {
			t.Fatalf("Failed to create module directory %s: %v", modulePath, err)
		}
		t.Logf("✓ Ensured module directory exists: %s", modulePath)
	}

	// Create directories for replacement modules
	for _, replacement := range lockFile.Replacements {
		// Find the corresponding module to get its hash
		var moduleHash string
		for _, module := range lockFile.Modules {
			if module.Name == replacement.From {
				moduleHash = module.Hash
				break
			}
		}

		if moduleHash != "" {
			// Parse the replacement name to get organization and module parts
			name, err := deps.ParseName(replacement.From)
			if err != nil {
				t.Fatalf("Failed to parse replacement name %s: %v", replacement.From, err)
			}

			// Create the directory structure: Organization/Module@Hash
			replacementDirName := name.Organization + "/" + name.Module + "@" + moduleHash
			replacementPath := filepath.Join(wippyDir, replacementDirName)

			if err := os.MkdirAll(replacementPath, 0755); err != nil {
				t.Fatalf("Failed to create replacement module directory %s: %v", replacementPath, err)
			}
			t.Logf("✓ Ensured replacement module directory exists: %s", replacementPath)
		}
	}
}
