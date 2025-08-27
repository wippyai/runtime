package env

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTestFile(t *testing.T) (string, func()) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "filestorage-test-*")
	require.NoError(t, err)

	// Create a test file with some initial content
	testFile := filepath.Join(tmpDir, "test.env")
	content := `KEY1=value1
KEY2=value2 # with comment
KEY3=value3`
	//nolint:gosec // ok for tests
	err = os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	// Verify the file was created and is readable
	_, err = os.Stat(testFile)
	require.NoError(t, err)

	// Return cleanup function
	cleanup := func() {
		// On Windows, we need to ensure files are fully closed before cleanup
		// Give the OS a moment to release file handles
		if runtime.GOOS == "windows" {
			time.Sleep(10 * time.Millisecond)
		}

		// Try to remove the test file first
		if err := os.Remove(testFile); err != nil {
			t.Logf("Failed to remove test file: %v", err)
		}

		// Then remove the directory
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove test directory: %v", err)
		}
	}

	return testFile, cleanup
}

func TestNewFileStorage(t *testing.T) {
	logger := zap.NewNop()
	filePath := "test.env"
	storage := NewFileStorage(filePath, true, 0644, 0755, logger)

	assert.NotNil(t, storage)
	assert.Equal(t, filePath, storage.filepath)
	assert.NotNil(t, storage.log)
	assert.Equal(t, true, storage.autoCreate)
	assert.Equal(t, os.FileMode(0644), storage.fileMode)
	assert.Equal(t, os.FileMode(0755), storage.dirMode)

	// Test that the storage can be started and stopped
	statusCh, err := storage.Start(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, statusCh)

	status := <-statusCh
	assert.Equal(t, supervisor.Running, status)

	err = storage.Stop(context.Background())
	assert.NoError(t, err)

	// Test that the storage can handle operations without a real file
	// (since we're just testing the constructor)
	_, err = storage.Get(context.Background(), "test")
	assert.Error(t, err) // Should fail because file doesn't exist

	// Test that the storage can handle List operation without a real file
	values, err := storage.List(context.Background())
	assert.Error(t, err) // Should fail because file doesn't exist
	assert.Nil(t, values)

	// Test that the storage can handle Set operation without a real file
	err = storage.Set(context.Background(), "test", "value")
	assert.Error(t, err) // Should fail because file doesn't exist

	// Test that the storage can handle Delete operation without a real file
	err = storage.Delete(context.Background(), "test")
	assert.Error(t, err) // Should fail because file doesn't exist

	// Test that the storage can handle operations with autoCreate disabled
	storageNoAuto := NewFileStorage(filePath, false, 0644, 0755, logger)
	assert.NotNil(t, storageNoAuto)
	assert.Equal(t, false, storageNoAuto.autoCreate)

	// Test that the storage can handle operations with custom modes
	storageCustom := NewFileStorage(filePath, true, 0600, 0700, logger)
	assert.NotNil(t, storageCustom)
	assert.Equal(t, os.FileMode(0600), storageCustom.fileMode)
	assert.Equal(t, os.FileMode(0700), storageCustom.dirMode)

	// Test that the storage can handle operations with default modes
	storageDefault := NewFileStorage(filePath, true, 0, 0, logger)
	assert.NotNil(t, storageDefault)
	assert.Equal(t, os.FileMode(0644), storageDefault.fileMode)
	assert.Equal(t, os.FileMode(0755), storageDefault.dirMode)

	// Test that the storage can handle operations with nil logger
	storageNilLogger := NewFileStorage(filePath, true, 0644, 0755, nil)
	assert.NotNil(t, storageNilLogger)
	assert.Nil(t, storageNilLogger.log)

	// Test that the storage can handle operations with empty filepath
	storageEmptyPath := NewFileStorage("", true, 0644, 0755, logger)
	assert.NotNil(t, storageEmptyPath)
	assert.Equal(t, "", storageEmptyPath.filepath)

	// Test that the storage can handle operations with absolute filepath
	absPath := filepath.Join(os.TempDir(), "test.env")
	storageAbsPath := NewFileStorage(absPath, true, 0644, 0755, logger)
	assert.NotNil(t, storageAbsPath)
	assert.Equal(t, absPath, storageAbsPath.filepath)

	// Test that the storage can handle operations with relative filepath
	relPath := "./test.env"
	storageRelPath := NewFileStorage(relPath, true, 0644, 0755, logger)
	assert.NotNil(t, storageRelPath)
	assert.Equal(t, relPath, storageRelPath.filepath)

	// Test that the storage can handle operations with filepath containing spaces
	spacePath := "test file.env"
	storageSpacePath := NewFileStorage(spacePath, true, 0644, 0755, logger)
	assert.NotNil(t, storageSpacePath)
	assert.Equal(t, spacePath, storageSpacePath.filepath)

	// Test that the storage can handle operations with filepath containing special characters
	specialPath := "test-file.env"
	storageSpecialPath := NewFileStorage(specialPath, true, 0644, 0755, logger)
	assert.NotNil(t, storageSpecialPath)
	assert.Equal(t, specialPath, storageSpecialPath.filepath)

	// Test that the storage can handle operations with filepath containing dots
	dotPath := "test.env.backup"
	storageDotPath := NewFileStorage(dotPath, true, 0644, 0755, logger)
	assert.NotNil(t, storageDotPath)
	assert.Equal(t, dotPath, storageDotPath.filepath)
}

func TestFileStorage_Get(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	logger := zap.NewNop()
	storage := NewFileStorage(testFile, true, 0644, 0755, logger)

	// Ensure the test file exists and is readable
	_, err := os.Stat(testFile)
	require.NoError(t, err)

	tests := []struct {
		name     string
		key      string
		expected string
		wantErr  bool
	}{
		{
			name:     "existing key",
			key:      "KEY1",
			expected: "value1",
			wantErr:  false,
		},
		{
			name:     "key with comment",
			key:      "KEY2",
			expected: "value2",
			wantErr:  false,
		},
		{
			name:     "non-existent key",
			key:      "NONEXISTENT",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := storage.Get(context.Background(), tt.key)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, "", value)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, value)
			}

			// Verify the file still exists after the operation
			_, err = os.Stat(testFile)
			assert.NoError(t, err)
		})
	}
}

func TestFileStorage_Set(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	logger := zap.NewNop()
	storage := NewFileStorage(testFile, true, 0644, 0755, logger)

	// Ensure the test file exists and is readable
	_, err := os.Stat(testFile)
	require.NoError(t, err)

	tests := []struct {
		name     string
		key      string
		value    string
		expected string
	}{
		{
			name:     "update existing key",
			key:      "KEY1",
			value:    "newvalue1",
			expected: "newvalue1",
		},
		{
			name:     "add new key",
			key:      "NEWKEY",
			value:    "newvalue",
			expected: "newvalue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.Set(context.Background(), tt.key, tt.value)
			assert.NoError(t, err)

			// On Windows, give the file system a moment to settle
			if runtime.GOOS == "windows" {
				time.Sleep(5 * time.Millisecond)
			}

			// Verify the file still exists after the operation
			_, err = os.Stat(testFile)
			assert.NoError(t, err)

			// Verify the value was set correctly
			value, err := storage.Get(context.Background(), tt.key)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, value)
		})
	}
}

func TestFileStorage_Delete(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	logger := zap.NewNop()
	storage := NewFileStorage(testFile, true, 0644, 0755, logger)

	// Ensure the test file exists and is readable
	_, err := os.Stat(testFile)
	require.NoError(t, err)

	// Verify the key exists before deletion
	value, err := storage.Get(context.Background(), "KEY1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)

	// Delete an existing key
	err = storage.Delete(context.Background(), "KEY1")
	assert.NoError(t, err)

	// On Windows, give the file system a moment to settle
	if runtime.GOOS == "windows" {
		time.Sleep(5 * time.Millisecond)
	}

	// Verify the file still exists after the operation
	_, err = os.Stat(testFile)
	assert.NoError(t, err)

	// Verify the key is gone
	_, err = storage.Get(context.Background(), "KEY1")
	assert.Error(t, err)
}

func TestFileStorage_List(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	logger := zap.NewNop()
	storage := NewFileStorage(testFile, true, 0644, 0755, logger)

	// Ensure the test file exists and is readable
	_, err := os.Stat(testFile)
	require.NoError(t, err)

	values, err := storage.List(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, values)
	assert.Greater(t, len(values), 0)

	// Verify the file still exists after the operation
	_, err = os.Stat(testFile)
	assert.NoError(t, err)

	// Verify specific keys exist
	assert.Equal(t, "value1", values["KEY1"])
	assert.Equal(t, "value2", values["KEY2"])
	assert.Equal(t, "value3", values["KEY3"])
}

func TestFileStorage_StartStop(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	logger := zap.NewNop()
	storage := NewFileStorage(testFile, true, 0644, 0755, logger)

	// Ensure the test file exists and is readable
	_, err := os.Stat(testFile)
	require.NoError(t, err)

	// Test Start
	statusCh, err := storage.Start(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, statusCh)

	// Verify status
	status := <-statusCh
	assert.Equal(t, supervisor.Running, status)

	// Test Stop
	err = storage.Stop(context.Background())
	assert.NoError(t, err)

	// Verify the file still exists after the operation
	_, err = os.Stat(testFile)
	assert.NoError(t, err)
}
