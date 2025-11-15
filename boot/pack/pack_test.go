package pack

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	systempayload "github.com/wippyai/runtime/system/payload"
)

func testMetadata(entryCount int) registry.Metadata {
	return registry.Metadata{
		"wippy_version": "test",
		"wippy_commit":  "abc123",
		"wippy_date":    "2024-01-01",
		"packed_at":     "2024-01-01T00:00:00Z",
		"entry_count":   entryCount,
	}
}

func TestPackUnpack(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	packer := New(transcoder)

	t.Run("round trip", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry1"),
				Kind: "test.kind",
				Meta: registry.Metadata{"key": "value"},
				Data: payload.New(map[string]any{"field": "data"}),
			},
			{
				ID:   registry.ParseID("test:entry2"),
				Kind: "test.kind",
				Meta: registry.Metadata{},
				Data: payload.New([]any{"a", "b", "c"}),
			},
		}

		tmpDir := t.TempDir()
		packPath := filepath.Join(tmpDir, "test.pack")

		// Pack
		file, err := os.Create(packPath)
		require.NoError(t, err)
		err = packer.Pack(entries, file, testMetadata(len(entries)))
		file.Close()
		require.NoError(t, err)

		// Verify file exists
		info, err := os.Stat(packPath)
		require.NoError(t, err)
		assert.Greater(t, info.Size(), int64(0))

		// Unpack
		file, err = os.Open(packPath)
		require.NoError(t, err)
		unpacked, _, err := packer.Unpack(file)
		file.Close()
		require.NoError(t, err)

		// Verify entries match
		require.Len(t, unpacked, len(entries))
		assert.Equal(t, entries[0].ID, unpacked[0].ID)
		assert.Equal(t, entries[0].Kind, unpacked[0].Kind)
		assert.Equal(t, entries[1].ID, unpacked[1].ID)
	})

	t.Run("empty entries", func(t *testing.T) {
		entries := []registry.Entry{}

		tmpDir := t.TempDir()
		packPath := filepath.Join(tmpDir, "empty.pack")

		file, err := os.Create(packPath)
		require.NoError(t, err)
		err = packer.Pack(entries, file, testMetadata(len(entries)))
		file.Close()
		require.NoError(t, err)

		file, err = os.Open(packPath)
		require.NoError(t, err)
		unpacked, _, err := packer.Unpack(file)
		file.Close()
		require.NoError(t, err)
		assert.Empty(t, unpacked)
	})

	t.Run("large entry set", func(t *testing.T) {
		entries := make([]registry.Entry, 100)
		for i := 0; i < 100; i++ {
			entries[i] = registry.Entry{
				ID:   registry.ParseID("test:" + string(rune('a'+i%26))),
				Kind: "test.kind",
				Meta: registry.Metadata{"index": i},
				Data: payload.New(map[string]any{
					"value": i,
					"data":  "some longer data to test compression",
				}),
			}
		}

		tmpDir := t.TempDir()
		packPath := filepath.Join(tmpDir, "large.pack")

		file, err := os.Create(packPath)
		require.NoError(t, err)
		err = packer.Pack(entries, file, testMetadata(len(entries)))
		file.Close()
		require.NoError(t, err)

		file, err = os.Open(packPath)
		require.NoError(t, err)
		unpacked, _, err := packer.Unpack(file)
		file.Close()
		require.NoError(t, err)
		assert.Len(t, unpacked, 100)
	})

	t.Run("nil data", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("test:nil"),
				Kind: "test.kind",
				Meta: nil,
				Data: nil,
			},
		}

		tmpDir := t.TempDir()
		packPath := filepath.Join(tmpDir, "nil.pack")

		file, err := os.Create(packPath)
		require.NoError(t, err)
		err = packer.Pack(entries, file, testMetadata(len(entries)))
		file.Close()
		require.NoError(t, err)

		file, err = os.Open(packPath)
		require.NoError(t, err)
		unpacked, _, err := packer.Unpack(file)
		file.Close()
		require.NoError(t, err)
		require.Len(t, unpacked, 1)
		assert.Equal(t, entries[0].ID, unpacked[0].ID)
	})

	t.Run("complex nested data", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("ns:dependency"),
				Kind: registry.KindNamespaceDependency,
				Meta: registry.Metadata{"parent": "root"},
				Data: payload.New(map[string]any{
					"component": "wippy/actor",
					"parameters": []any{
						map[string]any{
							"name":  "version",
							"value": "1.0.0",
						},
						map[string]any{
							"name": "config",
							"value": map[string]any{
								"nested": "value",
								"deep": map[string]any{
									"level": 3,
								},
							},
						},
					},
				}),
			},
		}

		tmpDir := t.TempDir()
		packPath := filepath.Join(tmpDir, "complex.pack")

		file, err := os.Create(packPath)
		require.NoError(t, err)
		err = packer.Pack(entries, file, testMetadata(len(entries)))
		file.Close()
		require.NoError(t, err)

		file, err = os.Open(packPath)
		require.NoError(t, err)
		unpacked, _, err := packer.Unpack(file)
		file.Close()
		require.NoError(t, err)
		require.Len(t, unpacked, 1)
		assert.Equal(t, entries[0].ID, unpacked[0].ID)
		assert.Equal(t, entries[0].Kind, unpacked[0].Kind)
	})
}

func TestUnpackErrors(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	packer := New(transcoder)

	t.Run("file does not exist", func(t *testing.T) {
		file, err := os.Open("/nonexistent/file.pack")
		if err == nil {
			defer file.Close()
		}
		assert.Error(t, err)
	})

	t.Run("invalid magic header", func(t *testing.T) {
		tmpDir := t.TempDir()
		badPath := filepath.Join(tmpDir, "bad.pack")

		var buf bytes.Buffer
		buf.WriteString("BADMAGIC")
		buf.WriteByte(version)
		buf.Write(make([]byte, 64))
		buf.WriteString("data")

		err := os.WriteFile(badPath, buf.Bytes(), 0644)
		require.NoError(t, err)

		file, err := os.Open(badPath)
		require.NoError(t, err)
		_, _, err = packer.Unpack(file)
		file.Close()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid magic header")
	})

	t.Run("unsupported version", func(t *testing.T) {
		tmpDir := t.TempDir()
		badPath := filepath.Join(tmpDir, "badversion.pack")

		var buf bytes.Buffer
		buf.WriteString(magic)
		buf.WriteByte(0x99)
		buf.Write(make([]byte, 64))
		buf.WriteString("data")

		err := os.WriteFile(badPath, buf.Bytes(), 0644)
		require.NoError(t, err)

		file, err := os.Open(badPath)
		require.NoError(t, err)
		_, _, err = packer.Unpack(file)
		file.Close()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported version")
	})

	t.Run("file too small", func(t *testing.T) {
		tmpDir := t.TempDir()
		tinyPath := filepath.Join(tmpDir, "tiny.pack")

		err := os.WriteFile(tinyPath, []byte("tiny"), 0644)
		require.NoError(t, err)

		file, err := os.Open(tinyPath)
		require.NoError(t, err)
		_, _, err = packer.Unpack(file)
		file.Close()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "data too small")
	})

	t.Run("hash mismatch - corrupted data", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: registry.Metadata{},
				Data: payload.New(map[string]any{"original": "data"}),
			},
		}

		tmpDir := t.TempDir()
		packPath := filepath.Join(tmpDir, "corrupt.pack")

		// Pack valid file
		file, err := os.Create(packPath)
		require.NoError(t, err)
		err = packer.Pack(entries, file, testMetadata(len(entries)))
		file.Close()
		require.NoError(t, err)

		// Corrupt the file by modifying compressed data
		data, err := os.ReadFile(packPath)
		require.NoError(t, err)

		// Flip a bit in the compressed data section
		data[len(magic)+1+hashLen+10] ^= 0xFF

		err = os.WriteFile(packPath, data, 0644)
		require.NoError(t, err)

		// Try to unpack - should fail hash verification
		file, err = os.Open(packPath)
		require.NoError(t, err)
		_, _, err = packer.Unpack(file)
		file.Close()
		assert.Error(t, err)
	})

	t.Run("invalid compressed data", func(t *testing.T) {
		tmpDir := t.TempDir()
		badPath := filepath.Join(tmpDir, "badcompress.pack")

		var buf bytes.Buffer
		buf.WriteString(magic)
		buf.WriteByte(version)
		buf.Write(make([]byte, 64))
		buf.WriteString("not valid zstd data")

		err := os.WriteFile(badPath, buf.Bytes(), 0644)
		require.NoError(t, err)

		file, err := os.Open(badPath)
		require.NoError(t, err)
		_, _, err = packer.Unpack(file)
		file.Close()
		assert.Error(t, err)
	})
}

func TestPackErrors(t *testing.T) {
	t.Run("invalid path", func(t *testing.T) {
		file, err := os.Create("/nonexistent/dir/file.pack")
		if err == nil {
			defer file.Close()
		}
		assert.Error(t, err)
	})
}

func TestCompression(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	packer := New(transcoder)

	t.Run("compression reduces size", func(t *testing.T) {
		// Create entries with repetitive data that compresses well
		entries := make([]registry.Entry, 50)
		for i := 0; i < 50; i++ {
			entries[i] = registry.Entry{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: registry.Metadata{
					"repeated": "this is repeated data that should compress well",
				},
				Data: payload.New(map[string]any{
					"value": "repeated value repeated value repeated value",
				}),
			}
		}

		tmpDir := t.TempDir()
		packPath := filepath.Join(tmpDir, "compressed.pack")

		file, err := os.Create(packPath)
		require.NoError(t, err)
		err = packer.Pack(entries, file, testMetadata(len(entries)))
		file.Close()
		require.NoError(t, err)

		info, err := os.Stat(packPath)
		require.NoError(t, err)

		// File should be reasonably small due to compression
		// 50 entries with repetitive data should compress to < 5KB
		assert.Less(t, info.Size(), int64(5000))
	})
}
