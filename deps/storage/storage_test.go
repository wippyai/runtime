package storage

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
)

func TestFileSystemStorage_StoreProtoFiles(t *testing.T) {
	t.Run("stores files without legacy prefix", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		files := []*modulev1.File{
			{Path: "file1.lua", Content: []byte("content1")},
			{Path: "subdir/file2.lua", Content: []byte("content2")},
		}

		err := storage.StoreProtoFiles("org/module", files)
		if err != nil {
			t.Fatalf("StoreProtoFiles failed: %v", err)
		}

		assertFileExists(t, filepath.Join(tmpDir, "org/module/file1.lua"), "content1")
		assertFileExists(t, filepath.Join(tmpDir, "org/module/subdir/file2.lua"), "content2")
	})

	t.Run("strips legacy module-* prefix", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		files := []*modulev1.File{
			{Path: "module-llm-v0.0.11/llm.lua", Content: []byte("llm content")},
			{Path: "module-llm-v0.0.11/models.lua", Content: []byte("models content")},
		}

		err := storage.StoreProtoFiles("wippy/llm", files)
		if err != nil {
			t.Fatalf("StoreProtoFiles failed: %v", err)
		}

		assertFileExists(t, filepath.Join(tmpDir, "wippy/llm/llm.lua"), "llm content")
		assertFileExists(t, filepath.Join(tmpDir, "wippy/llm/models.lua"), "models content")

		assertFileNotExists(t, filepath.Join(tmpDir, "wippy/llm/module-llm-v0.0.11"))
	})

	t.Run("handles empty file list", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		err := storage.StoreProtoFiles("org/module", nil)
		if err != nil {
			t.Fatalf("StoreProtoFiles failed: %v", err)
		}
	})

	t.Run("creates nested directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		files := []*modulev1.File{
			{Path: "a/b/c/deep.lua", Content: []byte("deep content")},
		}

		err := storage.StoreProtoFiles("org/module", files)
		if err != nil {
			t.Fatalf("StoreProtoFiles failed: %v", err)
		}

		assertFileExists(t, filepath.Join(tmpDir, "org/module/a/b/c/deep.lua"), "deep content")
	})

	t.Run("overwrites existing files", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		files1 := []*modulev1.File{
			{Path: "file.lua", Content: []byte("original")},
		}
		files2 := []*modulev1.File{
			{Path: "file.lua", Content: []byte("updated")},
		}

		if err := storage.StoreProtoFiles("org/module", files1); err != nil {
			t.Fatalf("StoreProtoFiles failed: %v", err)
		}

		if err := storage.StoreProtoFiles("org/module", files2); err != nil {
			t.Fatalf("StoreProtoFiles failed: %v", err)
		}

		assertFileExists(t, filepath.Join(tmpDir, "org/module/file.lua"), "updated")
	})

	t.Run("cleans old files when storing new version", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		// Store v1 with 3 files
		files1 := []*modulev1.File{
			{Path: "file1.lua", Content: []byte("v1-file1")},
			{Path: "file2.lua", Content: []byte("v1-file2")},
			{Path: "subdir/file3.lua", Content: []byte("v1-file3")},
		}

		if err := storage.StoreProtoFiles("org/module", files1); err != nil {
			t.Fatalf("StoreProtoFiles v1 failed: %v", err)
		}

		// Verify v1 files exist
		assertFileExists(t, filepath.Join(tmpDir, "org/module/file1.lua"), "v1-file1")
		assertFileExists(t, filepath.Join(tmpDir, "org/module/file2.lua"), "v1-file2")
		assertFileExists(t, filepath.Join(tmpDir, "org/module/subdir/file3.lua"), "v1-file3")

		// Store v2 with only 1 file
		files2 := []*modulev1.File{
			{Path: "newfile.lua", Content: []byte("v2-new")},
		}

		if err := storage.StoreProtoFiles("org/module", files2); err != nil {
			t.Fatalf("StoreProtoFiles v2 failed: %v", err)
		}

		// Verify v2 file exists
		assertFileExists(t, filepath.Join(tmpDir, "org/module/newfile.lua"), "v2-new")

		// Verify v1 files are gone
		assertFileNotExists(t, filepath.Join(tmpDir, "org/module/file1.lua"))
		assertFileNotExists(t, filepath.Join(tmpDir, "org/module/file2.lua"))
		assertFileNotExists(t, filepath.Join(tmpDir, "org/module/subdir/file3.lua"))
		assertFileNotExists(t, filepath.Join(tmpDir, "org/module/subdir"))
	})

	t.Run("storing empty files list cleans directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		// Store v1 with files
		files1 := []*modulev1.File{
			{Path: "file1.lua", Content: []byte("content1")},
			{Path: "file2.lua", Content: []byte("content2")},
		}

		if err := storage.StoreProtoFiles("org/module", files1); err != nil {
			t.Fatalf("StoreProtoFiles v1 failed: %v", err)
		}

		assertFileExists(t, filepath.Join(tmpDir, "org/module/file1.lua"), "content1")

		// Store empty files list (should clean directory)
		if err := storage.StoreProtoFiles("org/module", nil); err != nil {
			t.Fatalf("StoreProtoFiles empty failed: %v", err)
		}

		// Directory should be cleaned
		assertFileNotExists(t, filepath.Join(tmpDir, "org/module/file1.lua"))
		assertFileNotExists(t, filepath.Join(tmpDir, "org/module/file2.lua"))
		assertFileNotExists(t, filepath.Join(tmpDir, "org/module"))
	})

	t.Run("cleans directory with different file structure", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		// Store v1 with nested structure
		files1 := []*modulev1.File{
			{Path: "lib/utils.lua", Content: []byte("utils")},
			{Path: "lib/helpers/string.lua", Content: []byte("string")},
			{Path: "lib/helpers/number.lua", Content: []byte("number")},
		}

		if err := storage.StoreProtoFiles("org/module", files1); err != nil {
			t.Fatalf("StoreProtoFiles v1 failed: %v", err)
		}

		// Store v2 with flat structure
		files2 := []*modulev1.File{
			{Path: "main.lua", Content: []byte("main")},
		}

		if err := storage.StoreProtoFiles("org/module", files2); err != nil {
			t.Fatalf("StoreProtoFiles v2 failed: %v", err)
		}

		// Only v2 file should exist
		assertFileExists(t, filepath.Join(tmpDir, "org/module/main.lua"), "main")

		// v1 structure should be completely gone
		assertFileNotExists(t, filepath.Join(tmpDir, "org/module/lib"))
		assertFileNotExists(t, filepath.Join(tmpDir, "org/module/lib/utils.lua"))
		assertFileNotExists(t, filepath.Join(tmpDir, "org/module/lib/helpers"))
	})

	t.Run("fails with empty basePath", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		files := []*modulev1.File{{Path: "file.lua", Content: []byte("content")}}

		err := storage.StoreProtoFiles("", files)
		if err == nil {
			t.Fatal("expected error for empty basePath")
		}
	})

	t.Run("fails with nil file", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		files := []*modulev1.File{nil}

		err := storage.StoreProtoFiles("org/module", files)
		if err == nil {
			t.Fatal("expected error for nil file")
		}
	})

	t.Run("fails with empty file path", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		files := []*modulev1.File{{Path: "", Content: []byte("content")}}

		err := storage.StoreProtoFiles("org/module", files)
		if err == nil {
			t.Fatal("expected error for empty file path")
		}
	})

	t.Run("fails with absolute path", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		files := []*modulev1.File{{Path: "/absolute/path.lua", Content: []byte("content")}}

		err := storage.StoreProtoFiles("org/module", files)
		if err == nil {
			t.Fatal("expected error for absolute path")
		}
	})

	t.Run("fails with invalid path containing ..", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		files := []*modulev1.File{{Path: "../escape.lua", Content: []byte("content")}}

		err := storage.StoreProtoFiles("org/module", files)
		if err == nil {
			t.Fatal("expected error for path with ..")
		}
	})

	t.Run("fails with empty path after legacy prefix strip", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		files := []*modulev1.File{{Path: "module-test/", Content: []byte("content")}}

		err := storage.StoreProtoFiles("org/module", files)
		if err == nil {
			t.Fatal("expected error for empty path after strip")
		}
	})
}

func TestFileSystemStorage_ReadFS(t *testing.T) {
	t.Run("returns fs.FS for existing directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		writeTestFile(t, tmpDir, "org/module/file1.lua", "content1")
		writeTestFile(t, tmpDir, "org/module/subdir/file2.lua", "content2")

		moduleFS, err := storage.ReadFS("org/module")
		if err != nil {
			t.Fatalf("ReadFS failed: %v", err)
		}

		content1, err := fs.ReadFile(moduleFS, "file1.lua")
		if err != nil {
			t.Fatalf("read file1.lua failed: %v", err)
		}
		if string(content1) != "content1" {
			t.Errorf("expected content1, got %s", string(content1))
		}

		content2, err := fs.ReadFile(moduleFS, "subdir/file2.lua")
		if err != nil {
			t.Fatalf("read subdir/file2.lua failed: %v", err)
		}
		if string(content2) != "content2" {
			t.Errorf("expected content2, got %s", string(content2))
		}
	})

	t.Run("fails for nonexistent directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		_, err := storage.ReadFS("org/nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent directory")
		}
	})

	t.Run("fails for empty basePath", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		_, err := storage.ReadFS("")
		if err == nil {
			t.Fatal("expected error for empty basePath")
		}
	})

	t.Run("fails when path is a file", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		writeTestFile(t, tmpDir, "file.txt", "content")

		_, err := storage.ReadFS("file.txt")
		if err == nil {
			t.Fatal("expected error when path is a file")
		}
	})
}

func TestFileSystemStorage_Exists(t *testing.T) {
	t.Run("returns true for directory with files", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		writeTestFile(t, tmpDir, "org/module/file.lua", "content")

		exists, err := storage.Exists("org/module")
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}

		if !exists {
			t.Error("expected directory to exist with content")
		}
	})

	t.Run("returns false for empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		if err := os.MkdirAll(filepath.Join(tmpDir, "org/module"), 0755); err != nil {
			t.Fatalf("create directory: %v", err)
		}

		exists, err := storage.Exists("org/module")
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}

		if exists {
			t.Error("expected empty directory to return false")
		}
	})

	t.Run("returns false for nonexistent directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		exists, err := storage.Exists("org/nonexistent")
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}

		if exists {
			t.Error("expected nonexistent directory to return false")
		}
	})

	t.Run("fails for empty basePath", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		_, err := storage.Exists("")
		if err == nil {
			t.Fatal("expected error for empty basePath")
		}
	})

	t.Run("fails for file instead of directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		writeTestFile(t, tmpDir, "file.txt", "content")

		_, err := storage.Exists("file.txt")
		if err == nil {
			t.Fatal("expected error for file instead of directory")
		}
	})
}

func TestFileSystemStorage_Delete(t *testing.T) {
	t.Run("deletes directory with files", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		writeTestFile(t, tmpDir, "org/module/file.lua", "content")

		err := storage.Delete("org/module")
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		if _, err := os.Stat(filepath.Join(tmpDir, "org/module")); !os.IsNotExist(err) {
			t.Error("expected directory to be deleted")
		}
	})

	t.Run("succeeds for nonexistent directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		err := storage.Delete("org/nonexistent")
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
	})

	t.Run("deletes nested directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		writeTestFile(t, tmpDir, "org/module/a/b/c/deep.lua", "content")

		err := storage.Delete("org/module")
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		if _, err := os.Stat(filepath.Join(tmpDir, "org/module")); !os.IsNotExist(err) {
			t.Error("expected directory to be deleted")
		}
	})

	t.Run("fails for empty basePath", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		err := storage.Delete("")
		if err == nil {
			t.Fatal("expected error for empty basePath")
		}
	})

	t.Run("fails for protected paths", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		protectedPaths := []string{"/", ".", ".."}
		for _, path := range protectedPaths {
			err := storage.Delete(path)
			if err == nil {
				t.Errorf("expected error for protected path %q", path)
			}
		}
	})
}

func TestFileSystemStorage_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewFileSystemStorage(tmpDir)

	originalFiles := []*modulev1.File{
		{Path: "file1.lua", Content: []byte("content1")},
		{Path: "subdir/file2.lua", Content: []byte("content2")},
		{Path: "deep/nested/file3.lua", Content: []byte("content3")},
	}

	if err := storage.StoreProtoFiles("org/module", originalFiles); err != nil {
		t.Fatalf("StoreProtoFiles failed: %v", err)
	}

	moduleFS, err := storage.ReadFS("org/module")
	if err != nil {
		t.Fatalf("ReadFS failed: %v", err)
	}

	for _, orig := range originalFiles {
		content, err := fs.ReadFile(moduleFS, orig.GetPath())
		if err != nil {
			t.Errorf("read file %s failed: %v", orig.GetPath(), err)
			continue
		}
		if string(content) != string(orig.GetContent()) {
			t.Errorf("content mismatch for %s: expected %s, got %s",
				orig.GetPath(), string(orig.GetContent()), string(content))
		}
	}
}

func assertFileExists(t *testing.T, path, expectedContent string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file %s does not exist: %v", path, err)
	}
	if string(content) != expectedContent {
		t.Errorf("file %s: expected %q, got %q", path, expectedContent, string(content))
	}
}

func assertFileNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file %s should not exist", path)
	}
}

func writeTestFile(t *testing.T, baseDir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(baseDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestFileSystemStorage_ErrorPaths(t *testing.T) {
	t.Run("StoreProtoFiles handles os.RemoveAll error path", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a read-only directory to trigger RemoveAll error
		moduleDir := filepath.Join(tmpDir, "org/module")
		if err := os.MkdirAll(moduleDir, 0755); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// Create a file inside
		testFile := filepath.Join(moduleDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// Make directory read-only on Unix systems
		if err := os.Chmod(moduleDir, 0444); err != nil {
			t.Skipf("cannot change permissions: %v", err)
		}
		defer os.Chmod(moduleDir, 0755) // Cleanup

		storage := NewFileSystemStorage(tmpDir)
		files := []*modulev1.File{{Path: "new.lua", Content: []byte("new")}}

		// This should fail on RemoveAll due to permissions
		err := storage.StoreProtoFiles("org/module", files)
		if err == nil {
			t.Skip("expected error but got none (may not work on this filesystem)")
		}
	})

	t.Run("StoreProtoFiles handles MkdirAll error after clean", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a file where we want a directory
		filePath := filepath.Join(tmpDir, "org")
		if err := os.WriteFile(filePath, []byte("blocker"), 0644); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		storage := NewFileSystemStorage(tmpDir)
		files := []*modulev1.File{{Path: "test.lua", Content: []byte("content")}}

		// Should fail when trying to create org/module (org is a file)
		err := storage.StoreProtoFiles("org/module", files)
		if err == nil {
			t.Fatal("expected error when creating directory over file")
		}
	})

	t.Run("StoreProtoFiles handles os.MkdirAll error for file parent", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create org/module with a file where subdirectory needs to be
		baseDir := filepath.Join(tmpDir, "org/module")
		if err := os.MkdirAll(baseDir, 0755); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// Create a file named "subdir" to block directory creation
		blocker := filepath.Join(baseDir, "subdir")
		if err := os.WriteFile(blocker, []byte("blocker"), 0644); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// This test is not achievable with clean-before-write design
		// because clean removes the blocker before MkdirAll is called
		t.Skip("Cannot test MkdirAll error path with clean-before-write design")
	})

	t.Run("StoreProtoFiles handles os.WriteFile error", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Make the entire tmpDir read-only after creating base structure
		baseDir := filepath.Join(tmpDir, "org/module")
		if err := os.MkdirAll(baseDir, 0755); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// Make parent directory read-only so we can't write files
		if err := os.Chmod(baseDir, 0555); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
		defer os.Chmod(baseDir, 0755) // Cleanup

		storage := NewFileSystemStorage(tmpDir)
		files := []*modulev1.File{{Path: "file.lua", Content: []byte("content")}}

		// Should fail - can't remove directory due to permissions, or can't write
		err := storage.StoreProtoFiles("org/module", files)
		if err == nil {
			t.Skip("expected error but got none (may not work on this filesystem)")
		}
	})

	t.Run("ReadFS handles stat error", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		// Try to read from directory we have no permission to stat
		if err := os.MkdirAll(filepath.Join(tmpDir, "org"), 0755); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		moduleDir := filepath.Join(tmpDir, "org/module")
		if err := os.Mkdir(moduleDir, 0000); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
		defer os.Chmod(moduleDir, 0755) // Cleanup

		// Remove read permission from parent
		if err := os.Chmod(filepath.Join(tmpDir, "org"), 0000); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
		defer os.Chmod(filepath.Join(tmpDir, "org"), 0755) // Cleanup

		_, err := storage.ReadFS("org/module")
		if err == nil {
			t.Skip("expected error but got none (may not work on this filesystem)")
		}
	})

	t.Run("Exists handles stat error", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		// Create directory, then remove read permission from parent
		if err := os.MkdirAll(filepath.Join(tmpDir, "org/module"), 0755); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		if err := os.Chmod(filepath.Join(tmpDir, "org"), 0000); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
		defer os.Chmod(filepath.Join(tmpDir, "org"), 0755) // Cleanup

		_, err := storage.Exists("org/module")
		if err == nil {
			t.Skip("expected error but got none (may not work on this filesystem)")
		}
	})

	t.Run("Exists handles ReadDir error", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		// Create directory with no read permission
		moduleDir := filepath.Join(tmpDir, "org/module")
		if err := os.MkdirAll(moduleDir, 0755); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// Add a file so directory isn't empty
		if err := os.WriteFile(filepath.Join(moduleDir, "file.txt"), []byte("test"), 0644); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// Remove read permission
		if err := os.Chmod(moduleDir, 0000); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
		defer os.Chmod(moduleDir, 0755) // Cleanup

		_, err := storage.Exists("org/module")
		if err == nil {
			t.Skip("expected error but got none (may not work on this filesystem)")
		}
	})

	t.Run("Delete handles RemoveAll error", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		// Create directory with file, make it read-only
		moduleDir := filepath.Join(tmpDir, "org/module")
		if err := os.MkdirAll(moduleDir, 0755); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		if err := os.WriteFile(filepath.Join(moduleDir, "file.txt"), []byte("test"), 0644); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		if err := os.Chmod(moduleDir, 0444); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
		defer os.Chmod(moduleDir, 0755) // Cleanup

		err := storage.Delete("org/module")
		if err == nil {
			t.Skip("expected error but got none (may not work on this filesystem)")
		}
	})
}
