package directory

import (
	"github.com/ponyruntime/pony/tests/tempfiles"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"testing"

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
			fs, err := NewDirectoryFS(root, tt.mode)
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

	fs, err := NewDirectoryFS(root, 0755)
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

	fs, err := NewDirectoryFS(root, 0755)
	require.NoError(t, err)

	// Close filesystem
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
			name: "Remove",
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
