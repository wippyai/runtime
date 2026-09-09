// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/tests/tempfiles"
)

func requireReadOnly(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var pathErr *iofs.PathError
	require.ErrorAs(t, err, &pathErr)
	assert.ErrorIs(t, pathErr.Err, fsapi.ErrReadOnly)
}

func TestFS_ReadOnlyVolume(t *testing.T) {
	root, cleanup := tempfiles.TempDirWithFiles(t, "readonly_test", map[string]string{
		"file1.txt":      "content1",
		"dir1/file2.txt": "content2",
	})
	defer cleanup()

	// The mode grants everything; read-only refuses mutations regardless.
	fs, err := NewReadOnlyFS(root, 0755)
	require.NoError(t, err)
	defer func() { require.NoError(t, fs.Close()) }()
	assert.True(t, fs.ReadOnly())

	// Reads, stats and listings work, including chunked reads for streaming.
	f, err := fs.Open("file1.txt")
	require.NoError(t, err)
	buf := make([]byte, 3)
	n, err := f.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "con", string(buf[:n]))
	rest, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "tent1", string(rest))
	require.NoError(t, f.Close())

	info, err := fs.Stat("dir1/file2.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(8), info.Size())
	_, err = fs.Lstat("file1.txt")
	require.NoError(t, err)
	entries, err := fs.ReadDir("dir1")
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	rf, err := fs.OpenFile("file1.txt", os.O_RDONLY, 0)
	require.NoError(t, err)
	_, err = rf.Write([]byte("x"))
	assert.Error(t, err, "a handle from a read-only volume never writes")
	require.NoError(t, rf.Close())

	// Every mutation is refused at the boundary.
	for _, flag := range []int{os.O_WRONLY, os.O_RDWR, os.O_RDONLY | os.O_CREATE, os.O_RDONLY | os.O_TRUNC, os.O_RDONLY | os.O_APPEND} {
		_, err = fs.OpenFile("file1.txt", flag, 0644)
		requireReadOnly(t, err)
	}
	_, err = fs.OpenFile("created.txt", os.O_CREATE|os.O_WRONLY, 0644)
	requireReadOnly(t, err)
	requireReadOnly(t, fs.Remove("file1.txt"))
	requireReadOnly(t, fs.Mkdir("newdir", 0755))
	requireReadOnly(t, fs.Rename("file1.txt", "moved.txt"))
	requireReadOnly(t, fs.Truncate("file1.txt", 0))
	requireReadOnly(t, fs.Chtimes("file1.txt", time.Now(), time.Now()))

	// Nothing changed on disk.
	content, err := os.ReadFile(filepath.Join(root, "file1.txt"))
	require.NoError(t, err)
	assert.Equal(t, "content1", string(content))
	_, err = os.Stat(filepath.Join(root, "created.txt"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(root, "newdir"))
	assert.True(t, os.IsNotExist(err))
}

func TestFS_ReadOnlyVolume_NeverCreatesRoot(t *testing.T) {
	root, cleanup := tempfiles.TempDirWithFiles(t, "readonly_root", map[string]string{})
	defer cleanup()
	_, err := NewReadOnlyFS(filepath.Join(root, "absent"), 0755)
	require.Error(t, err)
}

func TestFactory_CreateFS_ReadOnly(t *testing.T) {
	root, cleanup := tempfiles.TempDirWithFiles(t, "readonly_factory", map[string]string{"file.txt": "x"})
	defer cleanup()
	created, err := NewFactory().CreateFS(CreateFSConfig{DirPath: root, Mode: 0755, ReadOnly: true})
	require.NoError(t, err)
	readOnly, ok := created.(*FS)
	require.True(t, ok)
	defer func() { _ = readOnly.Close() }()
	assert.True(t, readOnly.ReadOnly())
	_, err = created.OpenFile("file.txt", os.O_WRONLY, 0644)
	requireReadOnly(t, err)
	writable, err := NewFactory().CreateFS(CreateFSConfig{DirPath: root, Mode: 0755})
	require.NoError(t, err)
	writableFS, ok := writable.(*FS)
	require.True(t, ok)
	defer func() { _ = writableFS.Close() }()
	assert.False(t, writableFS.ReadOnly())
	wf, err := writable.OpenFile("file.txt", os.O_WRONLY|os.O_TRUNC, 0644)
	require.NoError(t, err)
	require.NoError(t, wf.Close())
}
