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
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	"github.com/wippyai/runtime/tests/tempfiles"
)

func requireReadOnly(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	assert.ErrorIs(t, err, fsapi.ErrReadOnly)
}

func TestFactory_ReadOnlyVolume(t *testing.T) {
	root, cleanup := tempfiles.TempDirWithFiles(t, "readonly_test", map[string]string{
		"file1.txt":      "content1",
		"dir1/file2.txt": "content2",
	})
	defer cleanup()

	filesystem, err := NewFactory().CreateFS(CreateFSConfig{
		DirPath:  root,
		Mode:     0755,
		ReadOnly: true,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, filesystem.(io.Closer).Close()) }()
	_, exposesHostPath := filesystem.(fsapi.HostPathFS)
	assert.False(t, exposesHostPath)

	file, err := filesystem.Open("file1.txt")
	require.NoError(t, err)
	buf := make([]byte, 3)
	n, err := file.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "con", string(buf[:n]))
	_, exposesChmod := file.(interface{ Chmod(os.FileMode) error })
	assert.False(t, exposesChmod)
	writer, exposesWrite := file.(io.Writer)
	require.True(t, exposesWrite)
	_, err = writer.Write([]byte("x"))
	requireReadOnly(t, err)
	require.NoError(t, file.Close())

	info, err := filesystem.Stat("dir1/file2.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(8), info.Size())
	_, err = filesystem.Lstat("file1.txt")
	require.NoError(t, err)
	entries, err := filesystem.ReadDir("dir1")
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	directoryFile, err := filesystem.Open("dir1")
	require.NoError(t, err)
	readDirectory, ok := directoryFile.(iofs.ReadDirFile)
	require.True(t, ok)
	entries, err = readDirectory.ReadDir(-1)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	require.NoError(t, directoryFile.Close())

	readFile, err := filesystem.OpenFile("file1.txt", os.O_RDONLY, 0)
	require.NoError(t, err)
	_, err = readFile.Seek(3, io.SeekStart)
	require.NoError(t, err)
	rest, err := io.ReadAll(readFile)
	require.NoError(t, err)
	assert.Equal(t, "tent1", string(rest))
	_, exposesChmod = readFile.(interface{ Chmod(os.FileMode) error })
	assert.False(t, exposesChmod)
	_, err = readFile.Write([]byte("x"))
	requireReadOnly(t, err)
	require.NoError(t, readFile.Close())

	for _, flag := range []int{
		os.O_WRONLY,
		os.O_RDWR,
		os.O_RDONLY | os.O_CREATE,
		os.O_RDONLY | os.O_TRUNC,
		os.O_RDONLY | os.O_APPEND,
		os.O_WRONLY | os.O_CREATE | os.O_EXCL,
	} {
		_, err = filesystem.OpenFile("file1.txt", flag, 0644)
		requireReadOnly(t, err)
		var pathErr *iofs.PathError
		require.ErrorAs(t, err, &pathErr)
		assert.Equal(t, "open", pathErr.Op)
	}
	_, err = filesystem.OpenFile("created.txt", os.O_CREATE|os.O_WRONLY, 0644)
	requireReadOnly(t, err)
	requireReadOnly(t, filesystem.Remove("file1.txt"))
	requireReadOnly(t, filesystem.Mkdir("newdir", 0755))
	requireReadOnly(t, filesystem.Rename("file1.txt", "moved.txt"))
	requireReadOnly(t, filesystem.Truncate("file1.txt", 0))
	requireReadOnly(t, filesystem.Chtimes("file1.txt", time.Now(), time.Now()))

	content, err := os.ReadFile(filepath.Join(root, "file1.txt"))
	require.NoError(t, err)
	assert.Equal(t, "content1", string(content))
	_, err = os.Stat(filepath.Join(root, "created.txt"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(root, "newdir"))
	assert.True(t, os.IsNotExist(err))
}

func TestFactory_ReadOnlyNeverCreatesRoot(t *testing.T) {
	root, cleanup := tempfiles.TempDirWithFiles(t, "readonly_root", map[string]string{})
	defer cleanup()
	absent := filepath.Join(root, "absent")

	_, err := NewFactory().CreateFS(CreateFSConfig{DirPath: absent, Mode: 0755, ReadOnly: true})
	require.Error(t, err)
	_, statErr := os.Stat(absent)
	assert.True(t, os.IsNotExist(statErr))
}

func TestFactory_RejectsReadOnlyAutoInit(t *testing.T) {
	_, err := NewFactory().CreateFS(CreateFSConfig{
		DirPath:  filepath.Join(t.TempDir(), "absent"),
		Mode:     0755,
		AutoInit: true,
		ReadOnly: true,
	})
	assert.ErrorIs(t, err, dirapi.ErrReadOnlyAutoInit)
}

func TestFactory_WritableVolumeRetainsHostPath(t *testing.T) {
	filesystem, err := NewFactory().CreateFS(CreateFSConfig{DirPath: t.TempDir(), Mode: 0755})
	require.NoError(t, err)
	defer func() { require.NoError(t, filesystem.(io.Closer).Close()) }()
	_, exposesHostPath := filesystem.(fsapi.HostPathFS)
	assert.True(t, exposesHostPath)
}

func TestFactory_ReadOnlyCloseClosesDirectory(t *testing.T) {
	filesystem, err := NewFactory().CreateFS(CreateFSConfig{
		DirPath:  t.TempDir(),
		Mode:     0755,
		ReadOnly: true,
	})
	require.NoError(t, err)
	require.NoError(t, filesystem.(io.Closer).Close())
	_, err = filesystem.Open(".")
	assert.ErrorIs(t, err, fsapi.ErrClosed)
}
