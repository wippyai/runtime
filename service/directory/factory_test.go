package directory

import (
	"io/fs"
	"testing"

	"github.com/ponyruntime/pony/api/service/directory"
	"github.com/ponyruntime/pony/tests/tempfiles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDirectoryFSFactory(t *testing.T) {
	factory := NewDirectoryFSFactory()
	assert.NotNil(t, factory, "Factory should not be nil")
	assert.IsType(t, &FSFactory{}, factory, "Factory should be of type *FSFactory")
}

func TestFSFactory_CreateFS(t *testing.T) {
	// Create a temp directory with test files
	root, cleanup := tempfiles.TempDirWithFiles(t, "factory_test", map[string]string{
		"file1.txt": "test content",
	})
	defer cleanup()

	factory := NewDirectoryFSFactory()

	t.Run("DirectoryFS", func(t *testing.T) {
		// Create a directory filesystem (empty type name)
		filesystem, err := factory.CreateFS("", root, 0755)
		require.NoError(t, err, "CreateFS should not return an error")
		require.NotNil(t, filesystem, "CreateFS should return a filesystem")

		// Verify the returned filesystem is a *FS
		dirFS, ok := filesystem.(*FS)
		assert.True(t, ok, "Returned filesystem should be a *FS")
		assert.Equal(t, root, dirFS.dirPath, "Directory path should match the provided path")

		// Test error case with invalid path
		_, err = factory.CreateFS("", "/nonexistent/invalid/path", fs.FileMode(0755))
		assert.Error(t, err, "CreateFS should return error for invalid path")
	})

	t.Run("EmbedFS", func(t *testing.T) {
		// Test creating an embed filesystem
		filesystem, err := factory.CreateFS(directory.TypeNameEmbed, "./", 0)
		require.NoError(t, err, "CreateFS should not return an error for embed filesystem")
		require.NotNil(t, filesystem, "CreateFS should return a filesystem for embed type")

		// Verify the returned filesystem is an *EmbedFS
		embedFS, ok := filesystem.(*ReadOnlyFS)
		assert.True(t, ok, "Returned filesystem should be an *EmbedFS")

		// Verify we can read files from the embed filesystem
		file, err := embedFS.Open(".gitkeep")
		assert.NoError(t, err, "Should be able to read directory from embed filesystem")
		assert.NotNil(t, file, "Should be able to read file from embed filesystem")

		// Test invalid subdirectory in embed filesystem
		_, err = factory.CreateFS(directory.TypeNameEmbed, "nonexistent", 0)
		assert.Error(t, err, "CreateFS should return error for invalid embed path")
	})

}
