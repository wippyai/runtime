package pack

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	systempayload "github.com/wippyai/runtime/system/payload"
)

func TestConcurrentReadSamePack(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	// Create test data
	entries := make([]registry.Entry, 100)
	for i := 0; i < 100; i++ {
		entries[i] = registry.Entry{
			ID:   registry.ParseID("test:entry"),
			Kind: "test.kind",
			Meta: registry.Metadata{"index": i},
			Data: payload.New(map[string]any{"value": i}),
		}
	}

	var buf bytes.Buffer
	err := pw.PackEntries(registry.Metadata{"test": "concurrent"}, entries, &buf)
	require.NoError(t, err)

	packData := buf.Bytes()

	t.Run("multiple goroutines reading same pack", func(t *testing.T) {
		const numGoroutines = 10

		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(_ int) {
				defer wg.Done()

				reader := bytes.NewReader(packData)
				pr, err := NewReader(reader, transcoder)
				if err != nil {
					errors <- err
					return
				}

				unpacked, err := pr.GetEntries()
				if err != nil {
					errors <- err
					return
				}

				if len(unpacked) != 100 {
					errors <- assert.AnError
					return
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			assert.NoError(t, err)
		}
	})

	t.Run("concurrent GetEntries calls", func(t *testing.T) {
		reader := bytes.NewReader(packData)
		pr, err := NewReader(reader, transcoder)
		require.NoError(t, err)

		const numGoroutines = 20
		var wg sync.WaitGroup
		results := make(chan int, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				entries, err := pr.GetEntries()
				if err != nil {
					results <- -1
					return
				}
				results <- len(entries)
			}()
		}

		wg.Wait()
		close(results)

		for count := range results {
			assert.Equal(t, 100, count)
		}
	})

	t.Run("concurrent GetMetadata calls", func(t *testing.T) {
		reader := bytes.NewReader(packData)
		pr, err := NewReader(reader, transcoder)
		require.NoError(t, err)

		const numGoroutines = 20
		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				meta, err := pr.GetMetadata()
				if err != nil {
					errors <- err
					return
				}

				if meta == nil {
					errors <- assert.AnError
					return
				}
			}()
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			assert.NoError(t, err)
		}
	})
}

func TestConcurrentFSAccess(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	tmpDir := t.TempDir()

	// Create multiple test files
	for i := 0; i < 10; i++ {
		filename := filepath.Join(tmpDir, "file"+string(rune('0'+i))+".png")
		content := "content for file " + string(rune('0'+i))
		require.NoError(t, os.WriteFile(filename, []byte(content), 0600))
	}

	fsys := os.DirFS(tmpDir)

	var buf bytes.Buffer
	err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
		registry.ParseID("test:concurrent-fs"), registry.Metadata{}, &buf)
	require.NoError(t, err)

	packData := buf.Bytes()

	t.Run("concurrent GetFS calls", func(t *testing.T) {
		reader := bytes.NewReader(packData)
		pr, err := NewReader(reader, transcoder)
		require.NoError(t, err)

		const numGoroutines = 15
		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				packFS, err := pr.GetFS(registry.ParseID("test:concurrent-fs"))
				if err != nil {
					errors <- err
					return
				}

				if packFS == nil {
					errors <- assert.AnError
					return
				}
			}()
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			assert.NoError(t, err)
		}
	})

	t.Run("concurrent file reads from packFS", func(t *testing.T) {
		reader := bytes.NewReader(packData)
		pr, err := NewReader(reader, transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:concurrent-fs"))
		require.NoError(t, err)

		const numGoroutines = 20
		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(fileIndex int) {
				defer wg.Done()

				filename := "file" + string(rune('0'+(fileIndex%10))) + ".png"
				file, err := packFS.Open(filename)
				if err != nil {
					errors <- err
					return
				}
				defer file.Close()

				_, err = io.ReadAll(file)
				if err != nil {
					errors <- err
					return
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			assert.NoError(t, err)
		}
	})

	t.Run("concurrent ReadDir calls", func(t *testing.T) {
		reader := bytes.NewReader(packData)
		pr, err := NewReader(reader, transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:concurrent-fs"))
		require.NoError(t, err)

		const numGoroutines = 15
		var wg sync.WaitGroup
		results := make(chan int, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				entries, err := packFS.ReadDir(".")
				if err != nil {
					results <- -1
					return
				}
				results <- len(entries)
			}()
		}

		wg.Wait()
		close(results)

		for count := range results {
			assert.Equal(t, 10, count)
		}
	})

	t.Run("mixed concurrent operations", func(t *testing.T) {
		// Note: This test may expose race conditions when run with -race flag
		// Known issue: GetMetadata() has a race on the metadata field
		reader := bytes.NewReader(packData)
		pr, err := NewReader(reader, transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:concurrent-fs"))
		require.NoError(t, err)

		const numGoroutines = 30
		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(opType int) {
				defer wg.Done()

				switch opType % 3 {
				case 0:
					// Read file
					file, err := packFS.Open("file0.png")
					if err != nil {
						errors <- err
						return
					}
					_, err = io.ReadAll(file)
					file.Close()
					if err != nil {
						errors <- err
						return
					}

				case 1:
					// ReadDir
					_, err := packFS.ReadDir(".")
					if err != nil {
						errors <- err
						return
					}

				case 2:
					// Get metadata
					_, err := pr.GetMetadata()
					if err != nil {
						errors <- err
						return
					}
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			assert.NoError(t, err)
		}
	})
}

func TestConcurrentEntryReads(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	entries := make([]registry.Entry, 50)
	for i := 0; i < 50; i++ {
		entries[i] = registry.Entry{
			ID:   registry.ParseID("test:entry"),
			Kind: "test.kind",
			Meta: registry.Metadata{"index": i},
			Data: payload.New(map[string]any{
				"value": i,
				"data":  "some test data",
			}),
		}
	}

	var buf bytes.Buffer
	err := pw.PackEntries(registry.Metadata{}, entries, &buf)
	require.NoError(t, err)

	packData := buf.Bytes()

	t.Run("concurrent entry reads", func(t *testing.T) {
		const numReaders = 25
		var wg sync.WaitGroup
		errors := make(chan error, numReaders)

		for i := 0; i < numReaders; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				reader := bytes.NewReader(packData)
				pr, err := NewReader(reader, transcoder)
				if err != nil {
					errors <- err
					return
				}

				entries, err := pr.GetEntries()
				if err != nil {
					errors <- err
					return
				}

				if len(entries) != 50 {
					errors <- assert.AnError
					return
				}

				// Access entry data
				if entries[0].Data == nil {
					errors <- assert.AnError
					return
				}
			}()
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			assert.NoError(t, err)
		}
	})
}

func TestConcurrentResourceLoading(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	tmpDir := t.TempDir()

	// Create test files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test1.png"), []byte("test1"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test2.png"), []byte("test2"), 0600))

	fsys := os.DirFS(tmpDir)

	var buf bytes.Buffer
	err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
		registry.ParseID("test:resource"), registry.Metadata{}, &buf)
	require.NoError(t, err)

	packData := buf.Bytes()

	t.Run("concurrent resource loading", func(t *testing.T) {
		reader := bytes.NewReader(packData)
		pr, err := NewReader(reader, transcoder)
		require.NoError(t, err)

		const numGoroutines = 20
		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				// This should trigger lazy loading of the resource
				packFS, err := pr.GetFS(registry.ParseID("test:resource"))
				if err != nil {
					errors <- err
					return
				}

				// Access a file to ensure resource is fully loaded
				file, err := packFS.Open("test1.png")
				if err != nil {
					errors <- err
					return
				}
				defer file.Close()

				_, err = io.ReadAll(file)
				if err != nil {
					errors <- err
					return
				}
			}()
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			assert.NoError(t, err)
		}
	})
}
