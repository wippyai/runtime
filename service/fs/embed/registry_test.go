// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/wapp"
)

func TestRegistry_NewRegistry(t *testing.T) {
	r := NewRegistry()
	require.NotNil(t, r)
	assert.NotNil(t, r.readers)
	assert.Nil(t, r.files)
}

func TestRegistry_Register(t *testing.T) {
	t.Run("empty pack path", func(t *testing.T) {
		r := NewRegistry()

		err := r.Register("", nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "packPath cannot be empty")
	})

	t.Run("nil reader", func(t *testing.T) {
		r := NewRegistry()

		err := r.Register("test/pack", nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reader cannot be nil")
	})

	t.Run("nil file is allowed", func(t *testing.T) {
		r := NewRegistry()
		reader := createTestReader(t)

		err := r.Register("test/pack", reader, nil)
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.readers, 1)
		assert.Len(t, r.files, 0)
		r.mu.RUnlock()
	})

	t.Run("tracks file handle", func(t *testing.T) {
		r := NewRegistry()
		reader, file := createTestReaderWithFile(t)
		defer file.Close()

		err := r.Register("test/pack", reader, file)
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.readers, 1)
		assert.Len(t, r.files, 1)
		r.mu.RUnlock()
	})

	t.Run("overwrites existing reader", func(t *testing.T) {
		r := NewRegistry()
		reader1 := createTestReader(t)
		reader2 := createTestReader(t)

		err := r.Register("test/pack", reader1, nil)
		require.NoError(t, err)

		err = r.Register("test/pack", reader2, nil)
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.readers, 1)
		assert.Equal(t, reader2, r.readers["test/pack"])
		r.mu.RUnlock()
	})
}

func TestRegistry_GetFS(t *testing.T) {
	t.Run("not found with no readers", func(t *testing.T) {
		r := NewRegistry()

		_, err := r.GetFS(registry.NewID("test", "notfound"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("not found with readers but no matching resource", func(t *testing.T) {
		r := NewRegistry()
		reader := createTestReader(t)
		err := r.Register("test/pack", reader, nil)
		require.NoError(t, err)

		_, err = r.GetFS(registry.NewID("nonexistent", "resource"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestRegistry_Close(t *testing.T) {
	t.Run("clears all readers", func(t *testing.T) {
		r := NewRegistry()
		reader := createTestReader(t)
		err := r.Register("test/pack", reader, nil)
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.readers, 1)
		r.mu.RUnlock()

		err = r.Close()
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.readers, 0)
		r.mu.RUnlock()
	})

	t.Run("closes tracked files", func(t *testing.T) {
		r := NewRegistry()
		reader, file := createTestReaderWithFile(t)

		err := r.Register("test/pack", reader, file)
		require.NoError(t, err)

		err = r.Close()
		require.NoError(t, err)

		// Verify file is closed by trying to read from it
		_, err = file.Read(make([]byte, 1))
		require.Error(t, err)
	})

	t.Run("idempotent", func(t *testing.T) {
		r := NewRegistry()

		err := r.Close()
		require.NoError(t, err)

		err = r.Close()
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.readers, 0)
		r.mu.RUnlock()
	})

	t.Run("clears files list", func(t *testing.T) {
		r := NewRegistry()
		reader, file := createTestReaderWithFile(t)

		err := r.Register("test/pack", reader, file)
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.files, 1)
		r.mu.RUnlock()

		err = r.Close()
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.files, 0)
		r.mu.RUnlock()
	})
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent register and getfs", func(t *testing.T) {
		r := NewRegistry()
		var wg sync.WaitGroup

		// Start multiple goroutines registering readers
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				reader := createTestReader(t)
				packPath := filepath.Join("test", "pack", string(rune('a'+idx)))
				_ = r.Register(packPath, reader, nil)
			}(i)
		}

		// Start multiple goroutines querying
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = r.GetFS(registry.NewID("test", "resource"))
			}()
		}

		wg.Wait()
	})

	t.Run("concurrent register same path", func(t *testing.T) {
		r := NewRegistry()
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				reader := createTestReader(t)
				_ = r.Register("same/path", reader, nil)
			}()
		}

		wg.Wait()

		r.mu.RLock()
		assert.Len(t, r.readers, 1)
		r.mu.RUnlock()
	})

	t.Run("concurrent close", func(t *testing.T) {
		r := NewRegistry()
		reader := createTestReader(t)
		err := r.Register("test/pack", reader, nil)
		require.NoError(t, err)

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = r.Close()
			}()
		}

		wg.Wait()
	})
}

func TestRegistry_MultipleReaders(t *testing.T) {
	t.Run("registers multiple readers", func(t *testing.T) {
		r := NewRegistry()

		for i := 0; i < 5; i++ {
			reader := createTestReader(t)
			packPath := filepath.Join("test", "pack", string(rune('a'+i))+".wapp")
			err := r.Register(packPath, reader, nil)
			require.NoError(t, err)
		}

		r.mu.RLock()
		assert.Len(t, r.readers, 5)
		r.mu.RUnlock()
	})

	t.Run("close clears all readers", func(t *testing.T) {
		r := NewRegistry()

		for i := 0; i < 5; i++ {
			reader := createTestReader(t)
			packPath := filepath.Join("test", "pack", string(rune('a'+i))+".wapp")
			err := r.Register(packPath, reader, nil)
			require.NoError(t, err)
		}

		err := r.Close()
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.readers, 0)
		r.mu.RUnlock()
	})
}

// createTestReader creates a minimal wapp reader for testing.
func createTestReader(t *testing.T) *wapp.Reader {
	t.Helper()

	// Create a minimal wapp in memory
	fsys := fstest.MapFS{
		"test.txt": &fstest.MapFile{Data: []byte("test"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := wapp.NewWriter()
	err := writer.PackEntries(wapp.Metadata{}, nil, &buf)
	require.NoError(t, err)

	// Write to temp file since wapp.Reader needs io.ReaderAt
	tmpDir := t.TempDir()
	wappPath := filepath.Join(tmpDir, "test.wapp")
	err = os.WriteFile(wappPath, buf.Bytes(), 0644)
	require.NoError(t, err)

	file, err := os.Open(wappPath)
	require.NoError(t, err)
	t.Cleanup(func() { file.Close() })

	reader, err := wapp.NewReader(file)
	require.NoError(t, err)

	_ = fsys // unused but kept for clarity
	return reader
}

// createTestReaderWithFile creates a wapp reader and returns the file handle.
func createTestReaderWithFile(t *testing.T) (*wapp.Reader, *os.File) {
	t.Helper()

	// Create a minimal wapp in memory
	var buf bytes.Buffer
	writer := wapp.NewWriter()
	err := writer.PackEntries(wapp.Metadata{}, nil, &buf)
	require.NoError(t, err)

	// Write to temp file since wapp.Reader needs io.ReaderAt
	tmpDir := t.TempDir()
	wappPath := filepath.Join(tmpDir, "test.wapp")
	err = os.WriteFile(wappPath, buf.Bytes(), 0644)
	require.NoError(t, err)

	file, err := os.Open(wappPath)
	require.NoError(t, err)

	reader, err := wapp.NewReader(file)
	require.NoError(t, err)

	return reader, file
}
