package env

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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

	// Return cleanup function
	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return testFile, cleanup
}

func TestNewFileStorage(t *testing.T) {
	logger := zap.NewNop()
	filepath := "test.env"
	storage := NewFileStorage(filepath, true, 0644, 0755, logger)

	assert.NotNil(t, storage)
	assert.Equal(t, filepath, storage.filepath)
	assert.NotNil(t, storage.log)
}

func TestFileStorage_Get(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	defer cleanup()

	logger := zap.NewNop()
	storage := NewFileStorage(testFile, true, 0644, 0755, logger)

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
		})
	}
}

func TestFileStorage_Set(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	defer cleanup()

	logger := zap.NewNop()
	storage := NewFileStorage(testFile, true, 0644, 0755, logger)

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

			// Verify the value was set correctly
			value, err := storage.Get(context.Background(), tt.key)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, value)
		})
	}
}

func TestFileStorage_Delete(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	defer cleanup()

	logger := zap.NewNop()
	storage := NewFileStorage(testFile, true, 0644, 0755, logger)

	// Delete an existing key
	err := storage.Delete(context.Background(), "KEY1")
	assert.NoError(t, err)

	// Verify the key is gone
	_, err = storage.Get(context.Background(), "KEY1")
	assert.Error(t, err)
}

func TestFileStorage_List(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	defer cleanup()

	logger := zap.NewNop()
	storage := NewFileStorage(testFile, true, 0644, 0755, logger)

	values, err := storage.List(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, values)
	assert.Greater(t, len(values), 0)
}

func TestFileStorage_StartStop(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	defer cleanup()

	logger := zap.NewNop()
	storage := NewFileStorage(testFile, true, 0644, 0755, logger)

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
}
