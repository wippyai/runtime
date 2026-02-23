// SPDX-License-Identifier: MPL-2.0

package file

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/env"
)

var _ env.Storage = (*Storage)(nil)

func setupTestFile(t *testing.T) (string, func()) {
	tmpDir, err := os.MkdirTemp("", "filestorage-test-*")
	require.NoError(t, err)

	testFile := filepath.Join(tmpDir, "test.env")
	content := `KEY1=value1
KEY2=value2 # with comment
KEY3=value3`

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
	filePath := "test.env"
	storage := NewStorage(filePath, true, 0644, 0755)

	assert.NotNil(t, storage)
	assert.Equal(t, filePath, storage.filepath)
	assert.Equal(t, true, storage.autoCreate)
	assert.Equal(t, os.FileMode(0644), storage.fileMode)
	assert.Equal(t, os.FileMode(0755), storage.dirMode)
}

func TestStorage_DefaultModes(t *testing.T) {
	storage := NewStorage("test.env", true, 0, 0)

	assert.Equal(t, os.FileMode(0644), storage.fileMode)
	assert.Equal(t, os.FileMode(0755), storage.dirMode)
}

func TestStorage_Get(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	storage := NewStorage(testFile, true, 0644, 0755)

	_, err := os.Stat(testFile)
	require.NoError(t, err)

	t.Run("existing key", func(t *testing.T) {
		value, err := storage.Get(context.Background(), "KEY1")
		require.NoError(t, err)
		assert.Equal(t, "value1", value)
	})

	t.Run("key with comment", func(t *testing.T) {
		value, err := storage.Get(context.Background(), "KEY2")
		require.NoError(t, err)
		assert.Equal(t, "value2", value)
	})

	t.Run("non-existent key", func(t *testing.T) {
		_, err := storage.Get(context.Background(), "NONEXISTENT")
		assert.ErrorIs(t, err, env.ErrVariableNotFound)
	})
}

func TestStorage_Set(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	storage := NewStorage(testFile, true, 0644, 0755)

	t.Run("update existing key", func(t *testing.T) {
		err := storage.Set(context.Background(), "KEY1", "newvalue1")
		require.NoError(t, err)

		if runtime.GOOS == "windows" {
			time.Sleep(5 * time.Millisecond)
		}

		value, err := storage.Get(context.Background(), "KEY1")
		require.NoError(t, err)
		assert.Equal(t, "newvalue1", value)
	})

	t.Run("add new key", func(t *testing.T) {
		err := storage.Set(context.Background(), "NEWKEY", "newvalue")
		require.NoError(t, err)

		if runtime.GOOS == "windows" {
			time.Sleep(5 * time.Millisecond)
		}

		value, err := storage.Get(context.Background(), "NEWKEY")
		require.NoError(t, err)
		assert.Equal(t, "newvalue", value)
	})
}

func TestStorage_Delete(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	storage := NewStorage(testFile, true, 0644, 0755)

	value, err := storage.Get(context.Background(), "KEY1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)

	err = storage.Delete(context.Background(), "KEY1")
	require.NoError(t, err)

	if runtime.GOOS == "windows" {
		time.Sleep(5 * time.Millisecond)
	}

	_, err = storage.Get(context.Background(), "KEY1")
	assert.ErrorIs(t, err, env.ErrVariableNotFound)
}

func TestStorage_List(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	storage := NewStorage(testFile, true, 0644, 0755)

	values, err := storage.List(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, values)
	assert.Greater(t, len(values), 0)

	assert.Equal(t, "value1", values["KEY1"])
	assert.Equal(t, "value2", values["KEY2"])
	assert.Equal(t, "value3", values["KEY3"])
}

func TestStorage_ListNonExistent(t *testing.T) {
	storage := NewStorage("/tmp/nonexistent-env-file.env", false, 0644, 0755)

	values, err := storage.List(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, values)
	assert.Empty(t, values)
}

func TestStorage_AutoCreate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "autocreate-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "subdir", "test.env")
	storage := NewStorage(testFile, true, 0644, 0755)

	err = storage.Set(context.Background(), "KEY1", "value1")
	require.NoError(t, err)

	value, err := storage.Get(context.Background(), "KEY1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)
}

func TestStorage_Concurrent(t *testing.T) {
	testFile, cleanup := setupTestFile(t)
	t.Cleanup(cleanup)

	storage := NewStorage(testFile, true, 0644, 0755)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)

		go func() {
			defer wg.Done()
			_, _ = storage.Get(ctx, "KEY1")
		}()

		go func() {
			defer wg.Done()
			_ = storage.Set(ctx, "KEY1", "value")
		}()

		go func() {
			defer wg.Done()
			_, _ = storage.List(ctx)
		}()
	}

	wg.Wait()
}

// Benchmarks

func BenchmarkStorage_Get(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "filestorage-bench-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.env")
	_ = os.WriteFile(testFile, []byte("KEY=value\n"), 0600)

	storage := NewStorage(testFile, true, 0600, 0700)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.Get(ctx, "KEY")
	}
}

func BenchmarkStorage_Set(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "filestorage-bench-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.env")
	_ = os.WriteFile(testFile, []byte("KEY=value\n"), 0600)

	storage := NewStorage(testFile, true, 0600, 0700)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = storage.Set(ctx, "KEY", "value")
	}
}

func BenchmarkStorage_List(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "filestorage-bench-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.env")
	content := "KEY1=value1\nKEY2=value2\nKEY3=value3\n"
	_ = os.WriteFile(testFile, []byte(content), 0600)

	storage := NewStorage(testFile, true, 0644, 0755)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.List(ctx)
	}
}
