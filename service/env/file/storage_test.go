package file

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTestFile(t *testing.T) (string, func()) {
	tmpDir, err := os.MkdirTemp("", "filestorage-test-*")
	require.NoError(t, err)

	testFile := filepath.Join(tmpDir, "test.env")
	content := `KEY1=value1
KEY2=value2 # with comment
KEY3=value3`
	//nolint:gosec
	err = os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	_, err = os.Stat(testFile)
	require.NoError(t, err)

	cleanup := func() {
		if runtime.GOOS == "windows" {
			time.Sleep(10 * time.Millisecond)
		}

		if err := os.Remove(testFile); err != nil {
			t.Logf("Failed to remove test file: %v", err)
		}

		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove test directory: %v", err)
		}
	}

	return testFile, cleanup
}

func TestNewStorage(t *testing.T) {
	logger := zap.NewNop()
	filePath := "test.env"
	storage := NewStorage(filePath, true, 0644, 0755, logger)

	assert.NotNil(t, storage)
	assert.Equal(t, filePath, storage.filepath)
	assert.NotNil(t, storage.log)
	assert.Equal(t, true, storage.autoCreate)
	assert.Equal(t, os.FileMode(0644), storage.fileMode)
	assert.Equal(t, os.FileMode(0755), storage.dirMode)
}

func TestStorage_DefaultModes(t *testing.T) {
	logger := zap.NewNop()
	storage := NewStorage("test.env", true, 0, 0, logger)

	assert.Equal(t, os.FileMode(0644), storage.fileMode)
	assert.Equal(t, os.FileMode(0755), storage.dirMode)
}

func TestStorage_Get(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	logger := zap.NewNop()
	storage := NewStorage(testFile, true, 0644, 0755, logger)

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

			_, err = os.Stat(testFile)
			assert.NoError(t, err)
		})
	}
}

func TestStorage_Set(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	logger := zap.NewNop()
	storage := NewStorage(testFile, true, 0644, 0755, logger)

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

			if runtime.GOOS == "windows" {
				time.Sleep(5 * time.Millisecond)
			}

			_, err = os.Stat(testFile)
			assert.NoError(t, err)

			value, err := storage.Get(context.Background(), tt.key)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, value)
		})
	}
}

func TestStorage_Delete(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	logger := zap.NewNop()
	storage := NewStorage(testFile, true, 0644, 0755, logger)

	_, err := os.Stat(testFile)
	require.NoError(t, err)

	value, err := storage.Get(context.Background(), "KEY1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)

	err = storage.Delete(context.Background(), "KEY1")
	assert.NoError(t, err)

	if runtime.GOOS == "windows" {
		time.Sleep(5 * time.Millisecond)
	}

	_, err = os.Stat(testFile)
	assert.NoError(t, err)

	_, err = storage.Get(context.Background(), "KEY1")
	assert.Error(t, err)
}

func TestStorage_List(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	logger := zap.NewNop()
	storage := NewStorage(testFile, true, 0644, 0755, logger)

	_, err := os.Stat(testFile)
	require.NoError(t, err)

	values, err := storage.List(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, values)
	assert.Greater(t, len(values), 0)

	_, err = os.Stat(testFile)
	assert.NoError(t, err)

	assert.Equal(t, "value1", values["KEY1"])
	assert.Equal(t, "value2", values["KEY2"])
	assert.Equal(t, "value3", values["KEY3"])
}

func TestStorage_ListNonExistent(t *testing.T) {
	logger := zap.NewNop()
	storage := NewStorage("/tmp/nonexistent-env-file.env", false, 0644, 0755, logger)

	values, err := storage.List(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, values)
	assert.Empty(t, values)
}

func TestStorage_AutoCreate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "autocreate-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "subdir", "test.env")
	logger := zap.NewNop()
	storage := NewStorage(testFile, true, 0644, 0755, logger)

	err = storage.Set(context.Background(), "KEY1", "value1")
	assert.NoError(t, err)

	value, err := storage.Get(context.Background(), "KEY1")
	assert.NoError(t, err)
	assert.Equal(t, "value1", value)
}
