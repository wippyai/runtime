package directory

import (
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/wippyai/runtime/tests/tempfiles"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFS(t *testing.T) {
	// Test different permission scenarios
	tests := []struct {
		name string
		mode os.FileMode
		test func(t *testing.T, fs *FS)
	}{
		{
			name: "ReadOnly",
			mode: 0444, // effective FS mode becomes 0500 (0400|0100)
			test: testReadOnlyFS,
		},
		{
			name: "ReadWrite",
			mode: 0644, // effective FS mode becomes 0700 (0600|0100)
			test: testReadWriteFS,
		},
		{
			name: "FullAccess",
			mode: 0755, // effective FS mode becomes 0700 (owner bits only)
			test: testFullAccessFS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory with test files
			root, cleanup := tempfiles.TempDirWithFiles(t, "fs_test", map[string]string{
				"file1.txt":      "content1",
				"dir1/file2.txt": "content2",
			})
			defer cleanup()

			// Create filesystem with specified mode
			fs, err := NewDirectoryFS(root, tt.mode, false)
			require.NoError(t, err)
			defer func() {
				require.NoError(t, fs.Close())
			}()

			tt.test(t, fs)
		})
	}
}

func testReadOnlyFS(t *testing.T, fs *FS) {
	// Test reading a file
	f, err := fs.Open("file1.txt")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, f.Close())
	}()

	content, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "content1", string(content))

	// Test directory listing
	entries, err := fs.ReadDir("dir1")
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "file2.txt", entries[0].Name())

	// Write operations should fail
	_, err = fs.OpenFile("newfile.txt", os.O_CREATE|os.O_WRONLY, 0644)
	assert.Error(t, err)
	var pathErr *iofs.PathError
	assert.ErrorAs(t, err, &pathErr)
	assert.ErrorIs(t, pathErr.Err, ErrPermissionDenied)

	err = fs.Remove("file1.txt")
	assert.Error(t, err)
	assert.ErrorAs(t, err, &pathErr)
	assert.ErrorIs(t, pathErr.Err, ErrPermissionDenied)
}

func testReadWriteFS(t *testing.T, fs *FS) {
	// Test reading a file
	f, err := fs.Open("file1.txt")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, f.Close())
	}()

	content, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "content1", string(content))

	// Test writing a new file
	wf, err := fs.OpenFile("newfile.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, wf.Close())
	}()

	_, err = wf.Write([]byte("new content"))
	require.NoError(t, err)

	// Verify written content
	content2, err := os.ReadFile(filepath.Join(fs.dirPath, "newfile.txt"))
	require.NoError(t, err)
	assert.Equal(t, "new content", string(content2))

	// Test file removal
	err = fs.Remove("newfile.txt")
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(fs.dirPath, "newfile.txt"))
	assert.True(t, os.IsNotExist(err))
}

func testFullAccessFS(t *testing.T, fs *FS) {
	// Test directory creation
	err := fs.Mkdir("newdir", 0755)
	require.NoError(t, err)

	// Test writing in new directory
	wf, err := fs.OpenFile("newdir/file.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, wf.Close())
	}()

	_, err = wf.Write([]byte("directory test"))
	require.NoError(t, err)

	// Test reading from new directory
	content, err := os.ReadFile(filepath.Join(fs.dirPath, "newdir/file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "directory test", string(content))

	// Test nested directory operations
	entries, err := fs.ReadDir("newdir")
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "file.txt", entries[0].Name())
}

func TestFS_Root(t *testing.T) {
	// This test verifies that using "/" works as expected.
	root, cleanup := tempfiles.TempDirWithFiles(t, "fs_root_test", map[string]string{
		"file1.txt":      "root content",
		"dir1/file2.txt": "nested content",
	})
	defer cleanup()

	fs, err := NewDirectoryFS(root, 0755, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, fs.Close())
	}()

	// "/" should refer to the FS's root (i.e. ".")
	info, err := fs.Stat("/")
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	entries, err := fs.ReadDir("/")
	require.NoError(t, err)
	// Expecting the two entries: "file1.txt" and "dir1"
	//nolint:prealloc // ok for now
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	assert.ElementsMatch(t, []string{"file1.txt", "dir1"}, names)
}

func TestFS_Closed(t *testing.T) {
	root, cleanup := tempfiles.TempDirWithFiles(t, "fs_closed_test", map[string]string{
		"test.txt": "test content",
	})
	defer cleanup()

	fs, err := NewDirectoryFS(root, 0755, false)
	require.NoError(t, err)

	// close filesystem
	require.NoError(t, fs.Close())

	// All operations should fail with ErrClosed
	operations := []struct {
		name string
		op   func() error
	}{
		{
			name: "Open",
			op: func() error {
				_, err := fs.Open("test.txt")
				return err
			},
		},
		{
			name: "OpenFile",
			op: func() error {
				_, err := fs.OpenFile("test.txt", os.O_RDONLY, 0644)
				return err
			},
		},
		{
			name: "ReadDir",
			op: func() error {
				_, err := fs.ReadDir(".")
				return err
			},
		},
		{
			name: "Done",
			op: func() error {
				return fs.Remove("test.txt")
			},
		},
		{
			name: "Mkdir",
			op: func() error {
				return fs.Mkdir("newdir", 0755)
			},
		},
		{
			name: "Stat",
			op: func() error {
				_, err := fs.Stat("test.txt")
				return err
			},
		},
	}

	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			err := op.op()
			assert.ErrorIs(t, err, ErrClosed)
		})
	}

	// Test that second close doesn't error
	err = fs.Close()
	require.NoError(t, err)
}

// Add these test methods to your fs_test.go file

func TestFS_NormalizePath(t *testing.T) {
	fs := &FS{}

	// Test root path "/"
	result := fs.normalizePath("/")
	assert.Equal(t, ".", result, "Root path '/' should be normalized to '.'")

	// Test path with leading slash
	result = fs.normalizePath("/some/path")
	assert.Equal(t, "some/path", result, "Path with leading slash should have it removed")

	// Test path without leading slash
	result = fs.normalizePath("already/normalized")
	assert.Equal(t, "already/normalized", result, "Path without leading slash should remain unchanged")
}

func TestNewDirectoryFS_WithPermissionAdjustment(t *testing.T) {
	// Test that read permissions automatically add execute permissions
	root, cleanup := tempfiles.TempDirWithFiles(t, "fs_perm_test", nil)
	defer cleanup()

	// Test with read-only permission 0444 (r--r--r--)
	fs, err := NewDirectoryFS(root, 0444, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, fs.Close())
	}()

	// Verify that execute bits were added (should be 0555 r-xr-xr-x)
	assert.Equal(t, os.FileMode(0555), fs.mode&0777, "Execute bits should be added to read-only mode")

	// Test with mixed read/write but no execute: 0644 (rw-r--r--)
	fs2, err := NewDirectoryFS(root, 0644, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, fs2.Close())
	}()

	// Verify that execute bits were added (should be 0755 rwxr-xr-x)
	assert.Equal(t, os.FileMode(0755), fs2.mode&0777, "Execute bits should be added when read bits are present")
}

func TestNewDirectoryFS_InvalidPath(t *testing.T) {
	// Using a path that should not exist on any system
	_, err := NewDirectoryFS("/path/that/cannot/possibly/exist/for/test", 0755, false)
	require.Error(t, err, "Should return error for invalid path")
	assert.Contains(t, err.Error(), "failed to open directory", "Error should mention the failure to open directory")
}

func TestFS_OpenErrorCases(t *testing.T) {
	// Test setup
	root, cleanup := tempfiles.TempDirWithFiles(t, "fs_open_test", map[string]string{
		"file1.txt": "content",
	})
	defer cleanup()

	// Test with read-only filesystem
	fs, err := NewDirectoryFS(root, 0400, false) // r--------
	require.NoError(t, err)

	// Test opening a directory without execute permission
	_, err = fs.Open(".")
	assert.Error(t, err, "Opening a directory without execute permission should fail")
	assert.Contains(t, err.Error(), "permission denied", "Error should mention permission denied")

	// Test opening a non-existent file
	_, err = fs.Open("nonexistent.txt")
	assert.Error(t, err, "Opening a non-existent file should return an error")

	// close the filesystem to test the closed state
	require.NoError(t, fs.Close())

	// Test opening any file on closed filesystem
	_, err = fs.Open("file1.txt")
	assert.ErrorIs(t, err, ErrClosed, "Opening a file on closed filesystem should return ErrClosed")
}

func TestFS_OpenFile_ExtraFlags(t *testing.T) {
	root, cleanup := tempfiles.TempDirWithFiles(t, "fs_openfile_test", map[string]string{
		"file1.txt": "content",
	})
	defer cleanup()

	fs, err := NewDirectoryFS(root, 0755, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, fs.Close())
	}()

	// Test with invalid permission bits (outside of fs.ModePerm)
	// fs.ModePerm is 0777, so add some higher-order bits
	_, err = fs.OpenFile("newfile.txt", os.O_CREATE|os.O_WRONLY, 0100000|0644)
	assert.Error(t, err, "Should reject file mode with bits outside fs.ModePerm")
	assert.Contains(t, err.Error(), "invalid file mode", "Error should mention invalid file mode")

	// Test read-write flag combination
	_, err = fs.OpenFile("file1.txt", os.O_RDWR, 0644)
	require.NoError(t, err, "Opening with RDWR should work with read+write permissions")

	// Test with read-only filesystem
	fsReadOnly, err := NewDirectoryFS(root, 0444, false) // r--r--r--
	require.NoError(t, err)
	defer func() {
		require.NoError(t, fsReadOnly.Close())
	}()

	// Try to open with write flag on read-only filesystem
	_, err = fsReadOnly.OpenFile("file1.txt", os.O_WRONLY, 0644)
	assert.Error(t, err, "Opening with write flag on read-only filesystem should fail")
	assert.Contains(t, err.Error(), "permission denied", "Error should mention permission denied")
}

func TestFS_ReadDir_ErrorCases(t *testing.T) {
	root, cleanup := tempfiles.TempDirWithFiles(t, "fs_readdir_test", map[string]string{
		"dir1/file1.txt": "content",
	})
	defer cleanup()

	// Create FS with read permission but no execute permission
	fs, err := NewDirectoryFS(root, 0400, false) // r--------
	require.NoError(t, err)
	defer func() {
		require.NoError(t, fs.Close())
	}()

	// ReadDir requires both read and execute permissions
	_, err = fs.ReadDir(".")
	assert.Error(t, err, "ReadDir without execute permission should fail")
	assert.Contains(t, err.Error(), "permission denied", "Error should mention permission denied")

	// Test non-existent directory
	_, err = fs.ReadDir("nonexistent")
	assert.Error(t, err, "ReadDir on non-existent directory should fail")

	// close and test closed state
	require.NoError(t, fs.Close())
	_, err = fs.ReadDir(".")
	assert.Error(t, err, "ReadDir on closed filesystem should fail")
	assert.ErrorIs(t, err, ErrClosed, "Error should be ErrClosed")
}

func TestFS_MkdirAndRemove_ErrorCases(t *testing.T) {
	root, cleanup := tempfiles.TempDirWithFiles(t, "fs_mkdir_test", nil)
	defer cleanup()

	// Test with read-only filesystem
	fs, err := NewDirectoryFS(root, 0444, false) // r--r--r--
	require.NoError(t, err)
	defer func() {
		require.NoError(t, fs.Close())
	}()

	// Mkdir requires write permission
	err = fs.Mkdir("newdir", 0755)
	if assert.Error(t, err, "Mkdir without write permission should fail") {
		assert.Contains(t, err.Error(), "permission denied", "Error should mention permission denied")
	}

	// Done requires write permission
	err = fs.Remove(".")
	if assert.Error(t, err, "Done without write permission should fail") {
		assert.Contains(t, err.Error(), "permission denied", "Error should mention permission denied")
	}

	// Create writable FS to test other error conditions
	fsRW, err := NewDirectoryFS(root, 0755, false)
	require.NoError(t, err)

	// Test removing non-existent file
	err = fsRW.Remove("nonexistent")
	assert.Error(t, err, "Removing non-existent file should fail")

	// close and test closed state
	require.NoError(t, fsRW.Close())
	err = fsRW.Mkdir("newdir", 0755)
	assert.Error(t, err, "Mkdir on closed filesystem should fail")
	assert.ErrorIs(t, err, ErrClosed, "Error should be ErrClosed")

	err = fsRW.Remove("something")
	assert.Error(t, err, "Done on closed filesystem should fail")
	assert.ErrorIs(t, err, ErrClosed, "Error should be ErrClosed")
}

// Test that Stat method properly handles error cases
func TestFS_Stat_ErrorCases(t *testing.T) {
	root, cleanup := tempfiles.TempDirWithFiles(t, "fs_stat_test", map[string]string{
		"file1.txt": "content",
	})
	defer cleanup()

	// Create FS with no read permission
	fs, err := NewDirectoryFS(root, 0, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, fs.Close())
	}()

	// Stat requires read permission
	_, err = fs.Stat("file1.txt")
	assert.Error(t, err, "Stat without read permission should fail")
	assert.Contains(t, err.Error(), "permission denied", "Error should mention permission denied")

	// Test non-existent file
	fsRW, err := NewDirectoryFS(root, 0755, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, fsRW.Close())
	}()

	_, err = fsRW.Stat("nonexistent")
	assert.Error(t, err, "Stat on non-existent file should fail")

	// close and test closed state
	require.NoError(t, fsRW.Close())
	_, err = fsRW.Stat("file1.txt")
	assert.Error(t, err, "Stat on closed filesystem should fail")
	assert.ErrorIs(t, err, ErrClosed, "Error should be ErrClosed")
}
