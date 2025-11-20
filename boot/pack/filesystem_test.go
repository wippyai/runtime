package pack

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	systempayload "github.com/wippyai/runtime/system/payload"
)

func TestFilesystemPack(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	t.Run("pack from os.DirFS with mixed file types", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create test files - use extensions that won't be compressed
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "data.png"), []byte("png file content"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "image.jpg"), []byte{0xFF, 0xD8, 0xFF, 0xE0}, 0600))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "archive.zip"), []byte{0x50, 0x4B, 0x03, 0x04}, 0600))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "binary.bin"), []byte{0x00, 0x01, 0x02, 0x03}, 0600))

		fsys := os.DirFS(tmpDir)

		metadata := registry.Metadata{"test": "filesystem"}
		var entries []registry.Entry
		resourceID := registry.ParseID("test:filesystem")
		resourceMeta := registry.Metadata{"type": "mixed"}

		var buf bytes.Buffer
		err := pw.Pack(metadata, entries, fsys, resourceID, resourceMeta, &buf)
		require.NoError(t, err)

		reader := bytes.NewReader(buf.Bytes())
		pr, err := NewReader(reader, transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(resourceID)
		require.NoError(t, err)

		// Verify files exist
		file, err := packFS.Open("data.png")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		content, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.Equal(t, "png file content", string(content))
	})

	t.Run("reading files back through packFS", func(t *testing.T) {
		tmpDir := t.TempDir()

		testContent := "hello world from pack"
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "hello.png"), []byte(testContent), 0600))

		fsys := os.DirFS(tmpDir)

		metadata := registry.Metadata{}
		var entries []registry.Entry
		resourceID := registry.ParseID("test:read")
		resourceMeta := registry.Metadata{}

		var buf bytes.Buffer
		err := pw.Pack(metadata, entries, fsys, resourceID, resourceMeta, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(resourceID)
		require.NoError(t, err)

		file, err := packFS.Open("hello.png")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		data, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.Equal(t, testContent, string(data))
	})

	t.Run("directory traversal", func(t *testing.T) {
		tmpDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "root.png"), []byte("root"), 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:dir"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:dir"))
		require.NoError(t, err)

		entries, err := packFS.ReadDir(".")
		require.NoError(t, err)
		assert.Len(t, entries, 1)
		assert.Equal(t, "root.png", entries[0].Name())
	})

	t.Run("file compression based on extension", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Text files should be compressed
		textData := strings.Repeat("compress me ", 100)
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "text.png"), []byte(textData), 0600))

		// Images should not be compressed
		imageData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "image.png"), imageData, 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:compress"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:compress"))
		require.NoError(t, err)

		// Read back png file
		txtFile, err := packFS.Open("text.png")
		require.NoError(t, err)
		txtContent, err := io.ReadAll(txtFile)
		txtFile.Close()
		require.NoError(t, err)
		assert.Equal(t, textData, string(txtContent))

		// Read back image file
		imgFile, err := packFS.Open("image.png")
		require.NoError(t, err)
		imgContent, err := io.ReadAll(imgFile)
		imgFile.Close()
		require.NoError(t, err)
		assert.Equal(t, imageData, imgContent)
	})

	t.Run("nested directories", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create nested structure
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "a", "b", "c"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a", "file1.png"), []byte("file1"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a", "b", "file2.png"), []byte("file2"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a", "b", "c", "file3.png"), []byte("file3"), 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:nested"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:nested"))
		require.NoError(t, err)

		// Read file from nested directory
		file, err := packFS.Open("a/b/c/file3.png")
		require.NoError(t, err)
		content, err := io.ReadAll(file)
		file.Close()
		require.NoError(t, err)
		assert.Equal(t, "file3", string(content))

		// Check directory listing
		entries, err := packFS.ReadDir("a/b")
		require.NoError(t, err)
		// Should have at least file2.png, may also have directory c
		assert.GreaterOrEqual(t, len(entries), 1)

		// Verify we can read the file
		found := false
		for _, e := range entries {
			if e.Name() == "file2.png" {
				found = true
				break
			}
		}
		assert.True(t, found, "Should find file2.png in directory listing")
	})

	t.Run("empty directories", func(t *testing.T) {
		tmpDir := t.TempDir()

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "empty"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file.png"), []byte("content"), 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:empty"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:empty"))
		require.NoError(t, err)

		entries, err := packFS.ReadDir("empty")
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("verify file mode and modtime preservation", func(t *testing.T) {
		tmpDir := t.TempDir()

		testFile := filepath.Join(tmpDir, "test.png")
		require.NoError(t, os.WriteFile(testFile, []byte("content"), 0600))

		// Set specific modtime
		testTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		require.NoError(t, os.Chtimes(testFile, testTime, testTime))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:mode"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:mode"))
		require.NoError(t, err)

		file, err := packFS.Open("test.png")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		info, err := file.Stat()
		require.NoError(t, err)

		// Mode should be preserved (at least the permission bits)
		assert.Equal(t, fs.FileMode(0600), info.Mode()&0777)

		// ModTime should be preserved (within 1 second tolerance)
		assert.True(t, info.ModTime().Unix() >= testTime.Unix()-1 &&
			info.ModTime().Unix() <= testTime.Unix()+1)
	})

	t.Run("files with spaces in names", func(t *testing.T) {
		tmpDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file with spaces.png"),
			[]byte("spaced content"), 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:spaces"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:spaces"))
		require.NoError(t, err)

		file, err := packFS.Open("file with spaces.png")
		require.NoError(t, err)
		content, err := io.ReadAll(file)
		file.Close()
		require.NoError(t, err)
		assert.Equal(t, "spaced content", string(content))
	})

	t.Run("symlinks handled gracefully", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a regular file
		targetFile := filepath.Join(tmpDir, "target.png")
		require.NoError(t, os.WriteFile(targetFile, []byte("target"), 0600))

		// Create a symlink
		linkFile := filepath.Join(tmpDir, "link.png")
		if err := os.Symlink("target.png", linkFile); err != nil {
			if !os.IsPermission(err) {
				t.Skip("Cannot create symlinks on this system")
			}
		}

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:symlink"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:symlink"))
		require.NoError(t, err)

		// Should be able to read the target file
		file, err := packFS.Open("target.png")
		require.NoError(t, err)
		content, err := io.ReadAll(file)
		file.Close()
		require.NoError(t, err)
		assert.Equal(t, "target", string(content))
	})

	t.Run("empty file", func(t *testing.T) {
		tmpDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "empty.png"), []byte{}, 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:empty-file"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:empty-file"))
		require.NoError(t, err)

		file, err := packFS.Open("empty.png")
		require.NoError(t, err)
		content, err := io.ReadAll(file)
		file.Close()
		require.NoError(t, err)
		assert.Empty(t, content)
	})

	t.Run("multiple file extensions", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Use only non-compressed extensions
		extensions := []string{
			".png", ".jpg", ".gif", ".webp", ".ico",
			".woff", ".woff2", ".ttf", ".mp3", ".zip",
		}

		for _, ext := range extensions {
			filename := "file" + ext
			require.NoError(t, os.WriteFile(filepath.Join(tmpDir, filename),
				[]byte("content for "+ext), 0600))
		}

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:extensions"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:extensions"))
		require.NoError(t, err)

		// Verify all files are accessible
		for _, ext := range extensions {
			filename := "file" + ext
			file, err := packFS.Open(filename)
			require.NoError(t, err, "Failed to open "+filename)
			content, err := io.ReadAll(file)
			file.Close()
			require.NoError(t, err)
			assert.Equal(t, "content for "+ext, string(content))
		}
	})
}

func TestPackFSErrors(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.png"), []byte("content"), 0600))

	fsys := os.DirFS(tmpDir)

	var buf bytes.Buffer
	err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
		registry.ParseID("test:errors"), registry.Metadata{}, &buf)
	require.NoError(t, err)

	pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
	require.NoError(t, err)

	packFS, err := pr.GetFS(registry.ParseID("test:errors"))
	require.NoError(t, err)

	t.Run("non-existent file", func(t *testing.T) {
		_, err := packFS.Open("nonexistent.png")
		assert.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("invalid path", func(t *testing.T) {
		_, err := packFS.Open("../invalid")
		assert.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrInvalid)
	})

	t.Run("read directory as file fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "dir"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "dir", "file.png"), []byte("content"), 0600))

		fsys := os.DirFS(tmpDir)

		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:dirread"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:dirread"))
		require.NoError(t, err)

		dirFile, err := packFS.Open("dir")
		require.NoError(t, err)
		_, err = io.ReadAll(dirFile)
		dirFile.Close()
		assert.Error(t, err)
	})

	t.Run("non-existent directory", func(t *testing.T) {
		_, err := packFS.ReadDir("nonexistent")
		assert.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})
}

func TestPackFileSeek(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	t.Run("basic seek operations", func(t *testing.T) {
		tmpDir := t.TempDir()
		testContent := "0123456789abcdefghij"
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.png"), []byte(testContent), 0600))

		fsys := os.DirFS(tmpDir)
		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:seek"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:seek"))
		require.NoError(t, err)

		file, err := packFS.Open("test.png")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		seeker, ok := file.(io.ReadSeeker)
		require.True(t, ok, "packFile must implement io.ReadSeeker")

		pos, err := seeker.Seek(5, io.SeekStart)
		require.NoError(t, err)
		assert.Equal(t, int64(5), pos)

		data := make([]byte, 5)
		n, err := seeker.Read(data)
		require.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "56789", string(data))
	})

	t.Run("seek from current position", func(t *testing.T) {
		tmpDir := t.TempDir()
		testContent := "0123456789abcdefghij"
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.png"), []byte(testContent), 0600))

		fsys := os.DirFS(tmpDir)
		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:seek-cur"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:seek-cur"))
		require.NoError(t, err)

		file, err := packFS.Open("test.png")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		seeker := file.(io.ReadSeeker)

		_, err = seeker.Seek(10, io.SeekStart)
		require.NoError(t, err)

		pos, err := seeker.Seek(5, io.SeekCurrent)
		require.NoError(t, err)
		assert.Equal(t, int64(15), pos)

		data := make([]byte, 3)
		n, err := seeker.Read(data)
		require.NoError(t, err)
		assert.Equal(t, 3, n)
		assert.Equal(t, "fgh", string(data))
	})

	t.Run("seek from end", func(t *testing.T) {
		tmpDir := t.TempDir()
		testContent := "0123456789abcdefghij"
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.png"), []byte(testContent), 0600))

		fsys := os.DirFS(tmpDir)
		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:seek-end"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:seek-end"))
		require.NoError(t, err)

		file, err := packFS.Open("test.png")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		seeker := file.(io.ReadSeeker)

		pos, err := seeker.Seek(-5, io.SeekEnd)
		require.NoError(t, err)
		assert.Equal(t, int64(15), pos)

		data := make([]byte, 10)
		n, err := seeker.Read(data)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("unexpected error: %v", err)
		}
		assert.Equal(t, 5, n)
		assert.Equal(t, "fghij", string(data[:n]))
	})

	t.Run("seek to start", func(t *testing.T) {
		tmpDir := t.TempDir()
		testContent := "hello world"
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.png"), []byte(testContent), 0600))

		fsys := os.DirFS(tmpDir)
		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:seek-start"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:seek-start"))
		require.NoError(t, err)

		file, err := packFS.Open("test.png")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		seeker := file.(io.ReadSeeker)

		data := make([]byte, 5)
		_, err = seeker.Read(data)
		require.NoError(t, err)

		pos, err := seeker.Seek(0, io.SeekStart)
		require.NoError(t, err)
		assert.Equal(t, int64(0), pos)

		_, err = seeker.Read(data)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(data))
	})

	t.Run("seek beyond file size", func(t *testing.T) {
		tmpDir := t.TempDir()
		testContent := "short"
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.png"), []byte(testContent), 0600))

		fsys := os.DirFS(tmpDir)
		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:seek-beyond"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:seek-beyond"))
		require.NoError(t, err)

		file, err := packFS.Open("test.png")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		seeker := file.(io.ReadSeeker)

		pos, err := seeker.Seek(100, io.SeekStart)
		require.NoError(t, err)
		assert.Equal(t, int64(100), pos)

		data := make([]byte, 10)
		n, err := seeker.Read(data)
		assert.Equal(t, 0, n)
		assert.Equal(t, io.EOF, err)
	})

	t.Run("negative position rejected", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.png"), []byte("content"), 0600))

		fsys := os.DirFS(tmpDir)
		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:seek-neg"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:seek-neg"))
		require.NoError(t, err)

		file, err := packFS.Open("test.png")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		seeker := file.(io.ReadSeeker)

		_, err = seeker.Seek(-10, io.SeekStart)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "negative position")
	})

	t.Run("invalid whence", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.png"), []byte("content"), 0600))

		fsys := os.DirFS(tmpDir)
		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:seek-whence"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:seek-whence"))
		require.NoError(t, err)

		file, err := packFS.Open("test.png")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		seeker := file.(io.ReadSeeker)

		_, err = seeker.Seek(0, 999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid whence")
	})

	t.Run("seek with compressed file", func(t *testing.T) {
		tmpDir := t.TempDir()
		testContent := strings.Repeat("0123456789", 50)
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte(testContent), 0600))

		fsys := os.DirFS(tmpDir)
		var buf bytes.Buffer
		err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
			registry.ParseID("test:seek-compressed"), registry.Metadata{}, &buf)
		require.NoError(t, err)

		pr, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		packFS, err := pr.GetFS(registry.ParseID("test:seek-compressed"))
		require.NoError(t, err)

		file, err := packFS.Open("test.txt")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		seeker := file.(io.ReadSeeker)

		pos, err := seeker.Seek(100, io.SeekStart)
		require.NoError(t, err)
		assert.Equal(t, int64(100), pos)

		data := make([]byte, 10)
		n, err := seeker.Read(data)
		require.NoError(t, err)
		assert.Equal(t, 10, n)
		assert.Equal(t, "0123456789", string(data))
	})
}
