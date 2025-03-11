package directory

import (
	"io/fs"
	"testing"

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

	// Create a filesystem
	filesystem, err := factory.CreateFS(root, 0755)
	require.NoError(t, err, "CreateFS should not return an error")
	require.NotNil(t, filesystem, "CreateFS should return a filesystem")

	// Verify the returned filesystem is a *FS
	_, ok := filesystem.(*FS)
	assert.True(t, ok, "Returned filesystem should be a *FS")

	// Test error case with invalid path
	_, err = factory.CreateFS("/nonexistent/invalid/path", fs.FileMode(0755))
	assert.Error(t, err, "CreateFS should return error for invalid path")
}
