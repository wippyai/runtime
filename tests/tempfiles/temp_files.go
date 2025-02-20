package tempfiles

import (
	"os"
	"path/filepath"
	"testing"
)

// TempDirWithFiles creates a temporary directory with the given file structure and content.
// It returns the path of the created directory and a cleanup function to remove the
// directory and all its content when done.
func TempDirWithFiles(t *testing.T, dirName string, files map[string]string) (string, func()) {
	rootDir, err := os.MkdirTemp("", dirName)
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	for filePath, content := range files {
		fullPath := filepath.Join(rootDir, filePath)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory '%s': %v", dir, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0600); err != nil {
			t.Fatalf("Failed to write file '%s': %v", fullPath, err)
		}
	}

	cleanup := func() {
		// todo: error handling
		_ = os.RemoveAll(rootDir)
	}
	return rootDir, cleanup
}
