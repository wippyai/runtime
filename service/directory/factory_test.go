package directory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/tests/tempfiles"
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
		config := CreateFSConfig{
			Name:     "",
			DirPath:  root,
			Mode:     0755,
			AutoInit: false,
		}
		filesystem, err := factory.CreateFS(config)
		require.NoError(t, err, "CreateFS should not return an error")
		require.NotNil(t, filesystem, "CreateFS should return a filesystem")

		// Verify the returned filesystem is a *FS
		dirFS, ok := filesystem.(*FS)
		assert.True(t, ok, "Returned filesystem should be a *FS")
		assert.Equal(t, root, dirFS.dirPath, "Directory path should match the provided path")

		// Test error case with invalid path
		invalidConfig := CreateFSConfig{
			Name:     "",
			DirPath:  "/nonexistent/invalid/path",
			Mode:     0755,
			AutoInit: false,
		}
		_, err = factory.CreateFS(invalidConfig)
		assert.Error(t, err, "CreateFS should return error for invalid path")

		// Test creating directory that doesn't exist
		createDirConfig := CreateFSConfig{
			Name:     "",
			DirPath:  root + "/new_dir",
			Mode:     0755,
			AutoInit: true,
		}
		newDirFS, err := factory.CreateFS(createDirConfig)
		require.NoError(t, err, "CreateFS should not return an error when creating a new directory")
		require.NotNil(t, newDirFS, "CreateFS should return a filesystem when creating a new directory")
	})
}
