package moduleloader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLockFileDeduplication(t *testing.T) {
	// Create a LockFile with duplicate modules
	lockFile := &LockFile{
		Directories: Directories{
			Modules: ".wippy",
			Src:     "src",
		},
		Modules: []LockedModule{
			{
				Name:    "wippy/actor",
				Version: "v0.2.1",
				Hash:    "hash1",
			},
			{
				Name:    "wippy/actor",
				Version: "v0.2.1",
				Hash:    "hash2", // Different hash, same name/version
			},
			{
				Name:    "wippy/test",
				Version: "v0.2.0",
				Hash:    "hash3",
			},
		},
	}

	// Save to a temporary file
	tempFile := filepath.Join(os.TempDir(), "test_dedup.lock")
	err := lockFile.SaveLockFile(tempFile)
	if err != nil {
		t.Fatalf("Failed to save lock file: %v", err)
	}

	// Load the file back
	loadedLockFile, err := LoadLockFile(tempFile)
	if err != nil {
		t.Fatalf("Failed to load lock file: %v", err)
	}

	// Should have only 2 unique modules
	if len(loadedLockFile.Modules) != 2 {
		t.Errorf("Expected 2 modules after deduplication, got %d", len(loadedLockFile.Modules))
	}

	// Check that we have the expected modules
	moduleNames := make(map[string]bool)
	for _, module := range loadedLockFile.Modules {
		moduleNames[module.Name] = true
	}

	if !moduleNames["wippy/actor"] {
		t.Error("Expected wippy/actor module")
	}
	if !moduleNames["wippy/test"] {
		t.Error("Expected wippy/test module")
	}

	// Clean up
	os.Remove(tempFile)
}
