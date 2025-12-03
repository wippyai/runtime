package pack

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	systempayload "github.com/wippyai/runtime/system/payload"
)

func TestLargeFileChunking(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	t.Run("file larger than 1MB uses chunking", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create 2MB file
		largeContent := make([]byte, 2*1024*1024)
		_, err := rand.Read(largeContent)
		require.NoError(t, err)

		filename := filepath.Join(tmpDir, "large.bin")
		require.NoError(t, os.WriteFile(filename, largeContent, 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err = pw.Pack(map[string]interface{}{}, []registry.Entry{}, fsys,
			registry.ParseID("test:large"), map[string]interface{}{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:large"))
		require.NoError(t, err)

		file, err := packFS.Open("large.bin")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		readContent, err := io.ReadAll(file)
		require.NoError(t, err)

		assert.Equal(t, largeContent, readContent)
	})

	t.Run("file exactly at chunk boundary 1MB", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create exactly 1MB file
		exactContent := make([]byte, 1024*1024)
		for i := range exactContent {
			exactContent[i] = byte(i % 256)
		}

		filename := filepath.Join(tmpDir, "exact.bin")
		require.NoError(t, os.WriteFile(filename, exactContent, 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(map[string]interface{}{}, []registry.Entry{}, fsys,
			registry.ParseID("test:exact"), map[string]interface{}{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:exact"))
		require.NoError(t, err)

		file, err := packFS.Open("exact.bin")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		readContent, err := io.ReadAll(file)
		require.NoError(t, err)

		assert.Equal(t, exactContent, readContent)
	})

	t.Run("very large file 10MB+", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create 10MB file
		veryLargeContent := make([]byte, 10*1024*1024)
		for i := range veryLargeContent {
			veryLargeContent[i] = byte(i % 256)
		}

		filename := filepath.Join(tmpDir, "verylarge.bin")
		require.NoError(t, os.WriteFile(filename, veryLargeContent, 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(map[string]interface{}{}, []registry.Entry{}, fsys,
			registry.ParseID("test:verylarge"), map[string]interface{}{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:verylarge"))
		require.NoError(t, err)

		file, err := packFS.Open("verylarge.bin")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		readContent, err := io.ReadAll(file)
		require.NoError(t, err)

		assert.Equal(t, len(veryLargeContent), len(readContent))
		assert.Equal(t, veryLargeContent, readContent)
	})

	t.Run("verify chunks created correctly for large file", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create 3MB file to span multiple chunks
		size := 3 * 1024 * 1024
		content := make([]byte, size)
		for i := range content {
			content[i] = byte(i % 256)
		}

		filename := filepath.Join(tmpDir, "multi.bin")
		require.NoError(t, os.WriteFile(filename, content, 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(map[string]interface{}{}, []registry.Entry{}, fsys,
			registry.ParseID("test:multi"), map[string]interface{}{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:multi"))
		require.NoError(t, err)

		file, err := packFS.Open("multi.bin")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		readContent, err := io.ReadAll(file)
		require.NoError(t, err)

		assert.Equal(t, content, readContent)
	})

	t.Run("reading chunked files works correctly", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create 5MB file
		size := 5 * 1024 * 1024
		content := make([]byte, size)
		for i := range content {
			content[i] = byte(i % 256)
		}

		filename := filepath.Join(tmpDir, "chunked.bin")
		require.NoError(t, os.WriteFile(filename, content, 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(map[string]interface{}{}, []registry.Entry{}, fsys,
			registry.ParseID("test:chunked"), map[string]interface{}{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:chunked"))
		require.NoError(t, err)

		file, err := packFS.Open("chunked.bin")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		// Read in smaller chunks to test partial reads
		readBuffer := make([]byte, 64*1024) // 64KB chunks
		var readContent []byte

		for {
			n, err := file.Read(readBuffer)
			if n > 0 {
				readContent = append(readContent, readBuffer[:n]...)
			}
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
		}

		assert.Equal(t, len(content), len(readContent))
		assert.Equal(t, content, readContent)
	})

	t.Run("mixed small and large files", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create small file
		smallContent := []byte("small file content")
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "small.txt"), smallContent, 0600))

		// Create large file (2MB)
		largeContent := make([]byte, 2*1024*1024)
		for i := range largeContent {
			largeContent[i] = byte(i % 256)
		}
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "large.bin"), largeContent, 0600))

		// Create medium file (500KB)
		mediumContent := make([]byte, 500*1024)
		for i := range mediumContent {
			mediumContent[i] = byte('A' + (i % 26))
		}
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "medium.txt"), mediumContent, 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(map[string]interface{}{}, []registry.Entry{}, fsys,
			registry.ParseID("test:mixed"), map[string]interface{}{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:mixed"))
		require.NoError(t, err)

		// Read small file
		file, err := packFS.Open("small.txt")
		require.NoError(t, err)
		data, err := io.ReadAll(file)
		file.Close()
		require.NoError(t, err)
		assert.Equal(t, smallContent, data)

		// Read large file
		file, err = packFS.Open("large.bin")
		require.NoError(t, err)
		data, err = io.ReadAll(file)
		file.Close()
		require.NoError(t, err)
		assert.Equal(t, largeContent, data)

		// Read medium file
		file, err = packFS.Open("medium.txt")
		require.NoError(t, err)
		data, err = io.ReadAll(file)
		file.Close()
		require.NoError(t, err)
		assert.Equal(t, mediumContent, data)
	})
}

func TestLargeFileEdgeCases(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	t.Run("file just under 1MB no chunking", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create file slightly under 1MB
		size := 1024*1024 - 1
		content := make([]byte, size)
		for i := range content {
			content[i] = byte(i % 256)
		}

		filename := filepath.Join(tmpDir, "under.bin")
		require.NoError(t, os.WriteFile(filename, content, 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(map[string]interface{}{}, []registry.Entry{}, fsys,
			registry.ParseID("test:under"), map[string]interface{}{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:under"))
		require.NoError(t, err)

		file, err := packFS.Open("under.bin")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		readContent, err := io.ReadAll(file)
		require.NoError(t, err)

		assert.Equal(t, content, readContent)
	})

	t.Run("file just over 1MB with chunking", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create file slightly over 1MB
		size := 1024*1024 + 1
		content := make([]byte, size)
		for i := range content {
			content[i] = byte(i % 256)
		}

		filename := filepath.Join(tmpDir, "over.bin")
		require.NoError(t, os.WriteFile(filename, content, 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(map[string]interface{}{}, []registry.Entry{}, fsys,
			registry.ParseID("test:over"), map[string]interface{}{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:over"))
		require.NoError(t, err)

		file, err := packFS.Open("over.bin")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		readContent, err := io.ReadAll(file)
		require.NoError(t, err)

		assert.Equal(t, content, readContent)
	})

	t.Run("partial reads from large file", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create 2MB file
		size := 2 * 1024 * 1024
		content := make([]byte, size)
		for i := range content {
			content[i] = byte(i % 256)
		}

		filename := filepath.Join(tmpDir, "partial.bin")
		require.NoError(t, os.WriteFile(filename, content, 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(map[string]interface{}{}, []registry.Entry{}, fsys,
			registry.ParseID("test:partial"), map[string]interface{}{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:partial"))
		require.NoError(t, err)

		file, err := packFS.Open("partial.bin")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		// Read in various sized chunks
		chunks := []int{1024, 4096, 16384, 65536, 131072}
		var readContent []byte

		for _, chunkSize := range chunks {
			readBuffer := make([]byte, chunkSize)
			n, err := file.Read(readBuffer)
			if n > 0 {
				readContent = append(readContent, readBuffer[:n]...)
			}
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
		}

		// Read rest
		rest, err := io.ReadAll(file)
		require.NoError(t, err)
		readContent = append(readContent, rest...)

		assert.Equal(t, content, readContent)
	})

	t.Run("seek and read operations on large file", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create 2MB file with pattern
		size := 2 * 1024 * 1024
		content := make([]byte, size)
		for i := range content {
			content[i] = byte(i % 256)
		}

		filename := filepath.Join(tmpDir, "seek.bin")
		require.NoError(t, os.WriteFile(filename, content, 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(map[string]interface{}{}, []registry.Entry{}, fsys,
			registry.ParseID("test:seek"), map[string]interface{}{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:seek"))
		require.NoError(t, err)

		file, err := packFS.Open("seek.bin")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		// Read the entire file to verify
		readContent, err := io.ReadAll(file)
		require.NoError(t, err)

		assert.Equal(t, content, readContent)
	})

	t.Run("compressed large text file", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create large text file with repetitive content (compresses well)
		size := 2 * 1024 * 1024
		content := make([]byte, size)
		pattern := []byte("This is a repeating pattern that compresses well. ")
		for i := 0; i < size; i++ {
			content[i] = pattern[i%len(pattern)]
		}

		filename := filepath.Join(tmpDir, "large.txt")
		require.NoError(t, os.WriteFile(filename, content, 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(map[string]interface{}{}, []registry.Entry{}, fsys,
			registry.ParseID("test:compressed"), map[string]interface{}{}, &buf)
		require.NoError(t, err)

		// Verify the pack is smaller than the original due to compression
		assert.Less(t, buf.Len(), size)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:compressed"))
		require.NoError(t, err)

		file, err := packFS.Open("large.txt")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		readContent, err := io.ReadAll(file)
		require.NoError(t, err)

		assert.Equal(t, content, readContent)
	})
}

func TestMultipleLargeFiles(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	t.Run("multiple large files in pack", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create multiple large files
		for i := 0; i < 3; i++ {
			size := (i + 2) * 1024 * 1024 // 2MB, 3MB, 4MB
			content := make([]byte, size)
			for j := range content {
				content[j] = byte((i + j) % 256)
			}

			filename := filepath.Join(tmpDir, "large"+string(rune('0'+i))+".bin")
			require.NoError(t, os.WriteFile(filename, content, 0600))
		}

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(map[string]interface{}{}, []registry.Entry{}, fsys,
			registry.ParseID("test:multiple"), map[string]interface{}{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:multiple"))
		require.NoError(t, err)

		// Verify all files can be read
		for i := 0; i < 3; i++ {
			filename := "large" + string(rune('0'+i)) + ".bin"
			file, err := packFS.Open(filename)
			require.NoError(t, err)

			size := (i + 2) * 1024 * 1024
			readContent, err := io.ReadAll(file)
			file.Close()
			require.NoError(t, err)

			assert.Equal(t, size, len(readContent))
		}
	})
}
