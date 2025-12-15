package pack

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	systempayload "github.com/wippyai/runtime/system/payload"
)

func testMetadata(entryCount int) map[string]interface{} {
	return map[string]interface{}{
		"wippy_version": "test",
		"wippy_commit":  "abc123",
		"wippy_date":    "2024-01-01",
		"packed_at":     "2024-01-01T00:00:00Z",
		"entry_count":   entryCount,
	}
}

func TestPackUnpack(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	packer := NewWriter(transcoder)

	t.Run("round trip", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry1"),
				Kind: "test.kind",
				Meta: map[string]interface{}{"key": "value"},
				Data: payload.New(map[string]any{"field": "data"}),
			},
			{
				ID:   registry.ParseID("test:entry2"),
				Kind: "test.kind",
				Meta: map[string]interface{}{},
				Data: payload.New([]any{"a", "b", "c"}),
			},
		}

		tmpDir := t.TempDir()
		packPath := filepath.Join(tmpDir, "test.pack")

		// Pack
		file, err := os.Create(packPath)
		require.NoError(t, err)
		err = packer.PackEntries(testMetadata(len(entries)), entries, file)
		_ = file.Close()
		require.NoError(t, err)

		// Verify file exists
		info, err := os.Stat(packPath)
		require.NoError(t, err)
		assert.Greater(t, info.Size(), int64(0))

		// Unpack
		file, err = os.Open(packPath)
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		data, err := io.ReadAll(file)
		require.NoError(t, err)

		reader, err := NewReader(bytes.NewReader(data), transcoder)
		require.NoError(t, err)

		unpacked, err := reader.GetEntries()
		require.NoError(t, err)

		// Verify entries match
		require.Len(t, unpacked, len(entries))
		assert.Equal(t, entries[0].ID, unpacked[0].ID)
		assert.Equal(t, entries[0].Kind, unpacked[0].Kind)
		assert.Equal(t, entries[1].ID, unpacked[1].ID)
	})

	t.Run("empty entries", func(t *testing.T) {
		var entries []registry.Entry

		tmpDir := t.TempDir()
		packPath := filepath.Join(tmpDir, "empty.pack")

		file, err := os.Create(packPath)
		require.NoError(t, err)
		err = packer.PackEntries(testMetadata(len(entries)), entries, file)
		_ = file.Close()
		require.NoError(t, err)

		file, err = os.Open(packPath)
		require.NoError(t, err)
		data, err := io.ReadAll(file)
		_ = file.Close()
		require.NoError(t, err)

		reader, err := NewReader(bytes.NewReader(data), transcoder)
		require.NoError(t, err)
		unpacked, err := reader.GetEntries()
		require.NoError(t, err)
		assert.Empty(t, unpacked)
	})

	t.Run("large entry set", func(t *testing.T) {
		entries := make([]registry.Entry, 100)
		for i := 0; i < 100; i++ {
			entries[i] = registry.Entry{
				ID:   registry.ParseID("test:" + string(rune('a'+i%26))),
				Kind: "test.kind",
				Meta: map[string]interface{}{"index": i},
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
		err = packer.PackEntries(testMetadata(len(entries)), entries, file)
		_ = file.Close()
		require.NoError(t, err)

		file, err = os.Open(packPath)
		require.NoError(t, err)
		data, err := io.ReadAll(file)
		_ = file.Close()
		require.NoError(t, err)

		reader, err := NewReader(bytes.NewReader(data), transcoder)
		require.NoError(t, err)
		unpacked, err := reader.GetEntries()
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
		err = packer.PackEntries(testMetadata(len(entries)), entries, file)
		_ = file.Close()
		require.NoError(t, err)

		file, err = os.Open(packPath)
		require.NoError(t, err)
		data, err := io.ReadAll(file)
		_ = file.Close()
		require.NoError(t, err)

		reader, err := NewReader(bytes.NewReader(data), transcoder)
		require.NoError(t, err)
		unpacked, err := reader.GetEntries()
		require.NoError(t, err)
		require.Len(t, unpacked, 1)
		assert.Equal(t, entries[0].ID, unpacked[0].ID)
	})

	t.Run("complex nested data", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("ns:dependency"),
				Kind: registry.NamespaceDependencyKind,
				Meta: map[string]interface{}{"parent": "root"},
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
		err = packer.PackEntries(testMetadata(len(entries)), entries, file)
		_ = file.Close()
		require.NoError(t, err)

		file, err = os.Open(packPath)
		require.NoError(t, err)
		data, err := io.ReadAll(file)
		_ = file.Close()
		require.NoError(t, err)

		reader, err := NewReader(bytes.NewReader(data), transcoder)
		require.NoError(t, err)
		unpacked, err := reader.GetEntries()
		require.NoError(t, err)
		require.Len(t, unpacked, 1)
		assert.Equal(t, entries[0].ID, unpacked[0].ID)
		assert.Equal(t, entries[0].Kind, unpacked[0].Kind)
	})
}

func TestUnpackErrors(t *testing.T) {
	transcoder := systempayload.NewTranscoder()

	t.Run("file does not exist", func(t *testing.T) {
		file, err := os.Open("/nonexistent/file.pack")
		if err == nil {
			defer func() { _ = file.Close() }()
		}
		assert.Error(t, err)
	})

	t.Run("invalid magic header", func(t *testing.T) {
		tmpDir := t.TempDir()
		badPath := filepath.Join(tmpDir, "bad.pack")

		var buf bytes.Buffer
		buf.WriteString("BADMAGIC")
		buf.WriteByte(version1)
		buf.Write(make([]byte, headerSize-9))
		buf.WriteString("data")

		err := os.WriteFile(badPath, buf.Bytes(), 0600)
		require.NoError(t, err)

		data, err := os.ReadFile(badPath)
		require.NoError(t, err)

		_, err = NewReader(bytes.NewReader(data), transcoder)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid magic")
	})

	t.Run("unsupported version", func(t *testing.T) {
		tmpDir := t.TempDir()
		badPath := filepath.Join(tmpDir, "badversion.pack")

		var buf bytes.Buffer
		buf.WriteString(magic)
		buf.WriteByte(0x99)
		buf.Write(make([]byte, headerSize-len(magic)-1))
		buf.WriteString("data")

		err := os.WriteFile(badPath, buf.Bytes(), 0600)
		require.NoError(t, err)

		data, err := os.ReadFile(badPath)
		require.NoError(t, err)

		_, err = NewReader(bytes.NewReader(data), transcoder)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported version")
	})

	t.Run("file too small", func(t *testing.T) {
		data := []byte("tiny")
		_, err := NewReader(bytes.NewReader(data), transcoder)
		assert.Error(t, err)
	})

	t.Run("hash mismatch - corrupted data", func(t *testing.T) {
		packer := NewWriter(transcoder)
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: map[string]interface{}{},
				Data: payload.New(map[string]any{"original": "data"}),
			},
		}

		tmpDir := t.TempDir()
		packPath := filepath.Join(tmpDir, "corrupt.pack")

		// Pack valid file
		file, err := os.Create(packPath)
		require.NoError(t, err)
		err = packer.PackEntries(testMetadata(len(entries)), entries, file)
		_ = file.Close()
		require.NoError(t, err)

		// Corrupt the file by modifying data
		data, err := os.ReadFile(packPath)
		require.NoError(t, err)

		// Flip a bit in the data section (beyond header)
		if len(data) > headerSize+10 {
			data[headerSize+10] ^= 0xFF
		}

		err = os.WriteFile(packPath, data, 0600)
		require.NoError(t, err)

		// Try to unpack - should fail
		corruptData, err := os.ReadFile(packPath)
		require.NoError(t, err)
		_, err = NewReader(bytes.NewReader(corruptData), transcoder)
		assert.Error(t, err)
	})

	t.Run("invalid compressed data", func(t *testing.T) {
		var buf bytes.Buffer
		buf.WriteString(magic)
		buf.WriteByte(version1)
		buf.Write(make([]byte, 64))
		buf.WriteString("not valid zstd data")

		_, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		assert.Error(t, err)
	})
}

func TestPackErrors(t *testing.T) {
	t.Run("invalid path", func(t *testing.T) {
		file, err := os.Create("/nonexistent/dir/file.pack")
		if err == nil {
			defer func() { _ = file.Close() }()
		}
		assert.Error(t, err)
	})
}

func TestCompression(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	packer := NewWriter(transcoder)

	t.Run("compression reduces size", func(t *testing.T) {
		// Create entries with repetitive data that compresses well
		entries := make([]registry.Entry, 50)
		for i := 0; i < 50; i++ {
			entries[i] = registry.Entry{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: map[string]interface{}{
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
		err = packer.PackEntries(testMetadata(len(entries)), entries, file)
		_ = file.Close()
		require.NoError(t, err)

		info, err := os.Stat(packPath)
		require.NoError(t, err)

		// File should be reasonably small due to compression
		// 50 entries with repetitive data should compress to < 5KB
		assert.Less(t, info.Size(), int64(5000))
	})
}

func TestUnpackBytes(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	packer := NewWriter(transcoder)

	t.Run("successful unpack from bytes", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry1"),
				Kind: "process.lua",
				Meta: map[string]interface{}{"version": "1.0"},
				Data: payload.New(map[string]any{"code": "return 42"}),
			},
		}

		var buf bytes.Buffer
		err := packer.PackEntries(testMetadata(len(entries)), entries, &buf)
		require.NoError(t, err)

		reader, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		unpacked, err := reader.GetEntries()
		require.NoError(t, err)

		meta, err := reader.GetMetadata()
		require.NoError(t, err)
		require.NotNil(t, meta)

		assert.Len(t, unpacked, 1)
		assert.Equal(t, entries[0].ID, unpacked[0].ID)
		assert.Equal(t, entries[0].Kind, unpacked[0].Kind)
	})

	t.Run("data too small", func(t *testing.T) {
		_, err := NewReader(bytes.NewReader([]byte("tiny")), transcoder)
		assert.Error(t, err)
	})

	t.Run("invalid magic header", func(t *testing.T) {
		badData := make([]byte, headerSize+20)
		copy(badData, "BADMAGIC")
		_, err := NewReader(bytes.NewReader(badData), transcoder)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid magic")
	})

	t.Run("unsupported version", func(t *testing.T) {
		badData := make([]byte, headerSize+20)
		copy(badData, magic)
		badData[len(magic)] = 99
		_, err := NewReader(bytes.NewReader(badData), transcoder)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported version")
	})

	t.Run("invalid compressed data", func(t *testing.T) {
		badData := make([]byte, headerSize+20)
		copy(badData, magic)
		badData[len(magic)] = version1
		copy(badData[headerSize:], "not valid zstd")

		_, err := NewReader(bytes.NewReader(badData), transcoder)
		assert.Error(t, err)
	})

	t.Run("corrupted data fails checksum", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("test:data"),
				Kind: "test.kind",
				Data: payload.New(map[string]any{"value": "original"}),
			},
		}

		var buf bytes.Buffer
		err := packer.PackEntries(testMetadata(len(entries)), entries, &buf)
		require.NoError(t, err)

		packedBytes := buf.Bytes()
		if len(packedBytes) > headerSize+5 {
			packedBytes[headerSize+5] ^= 0xFF
		}

		_, err = NewReader(bytes.NewReader(packedBytes), transcoder)
		assert.Error(t, err)
	})

	t.Run("empty entries pack and unpack", func(t *testing.T) {
		var entries []registry.Entry

		var buf bytes.Buffer
		err := packer.PackEntries(testMetadata(0), entries, &buf)
		require.NoError(t, err)

		reader, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)

		unpacked, err := reader.GetEntries()
		require.NoError(t, err)
		assert.Empty(t, unpacked)

		meta, err := reader.GetMetadata()
		require.NoError(t, err)
		assert.NotNil(t, meta)
	})
}

func TestNormalizePayload(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	packer := NewWriter(transcoder)

	t.Run("normalizes golang payload", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ParseID("test:entry"),
			Kind: "test.kind",
			Data: payload.New(map[string]any{"key": "value"}),
		}

		normalized, err := packer.normalizeEntry(entry)
		require.NoError(t, err)
		assert.NotNil(t, normalized.Data)
		assert.Equal(t, payload.Golang, normalized.Data.Format)
	})

	t.Run("Error format unchanged", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ParseID("test:entry"),
			Kind: "test.kind",
			Data: payload.NewPayload([]byte("error message"), payload.GoError),
		}

		normalized, err := packer.normalizeEntry(entry)
		require.NoError(t, err)
		assert.Equal(t, payload.GoError, normalized.Data.Format)
	})

	t.Run("String format unchanged", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ParseID("test:entry"),
			Kind: "test.kind",
			Data: payload.NewPayload([]byte("test string"), payload.String),
		}

		normalized, err := packer.normalizeEntry(entry)
		require.NoError(t, err)
		assert.Equal(t, payload.String, normalized.Data.Format)
	})

	t.Run("Bytes format unchanged", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ParseID("test:entry"),
			Kind: "test.kind",
			Data: payload.NewPayload([]byte{0x01, 0x02, 0x03}, payload.Bytes),
		}

		normalized, err := packer.normalizeEntry(entry)
		require.NoError(t, err)
		assert.Equal(t, payload.Bytes, normalized.Data.Format)
	})

	t.Run("handles nil payload", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ParseID("test:entry"),
			Kind: "test.kind",
			Data: nil,
		}

		normalized, err := packer.normalizeEntry(entry)
		require.NoError(t, err)
		assert.Nil(t, normalized.Data)
	})

	t.Run("handles complex nested data", func(t *testing.T) {
		complexData := map[string]any{
			"nested": map[string]any{
				"level1": map[string]any{
					"level2": []any{1, 2, 3},
				},
			},
			"array": []any{"a", "b", "c"},
		}

		entry := registry.Entry{
			ID:   registry.ParseID("test:entry"),
			Kind: "test.kind",
			Data: payload.New(complexData),
		}

		normalized, err := packer.normalizeEntry(entry)
		require.NoError(t, err)
		assert.Equal(t, payload.Golang, normalized.Data.Format)
		assert.NotNil(t, normalized.Data.Data)
	})
}

func TestPackEdgeCases(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	packer := NewWriter(transcoder)

	t.Run("large number of entries", func(t *testing.T) {
		entries := make([]registry.Entry, 1000)
		for i := 0; i < 1000; i++ {
			entries[i] = registry.Entry{
				ID:   registry.ParseID(fmt.Sprintf("test:entry%d", i)),
				Kind: "test.kind",
				Meta: map[string]interface{}{},
				Data: payload.New(map[string]any{"index": i}),
			}
		}

		var buf bytes.Buffer
		err := packer.PackEntries(testMetadata(len(entries)), entries, &buf)
		require.NoError(t, err)

		reader, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)
		unpacked, err := reader.GetEntries()
		require.NoError(t, err)
		assert.Len(t, unpacked, 1000)
	})

	t.Run("entries with empty metadata", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ParseID("test:entry"),
			Kind: "test.kind",
			Meta: map[string]interface{}{},
			Data: payload.New(map[string]any{"value": 42}),
		}

		var buf bytes.Buffer
		err := packer.PackEntries(map[string]interface{}{}, []registry.Entry{entry}, &buf)
		require.NoError(t, err)

		reader, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)
		unpacked, err := reader.GetEntries()
		require.NoError(t, err)
		assert.Len(t, unpacked, 1)

		meta, err := reader.GetMetadata()
		require.NoError(t, err)
		assert.NotNil(t, meta)
	})

	t.Run("nil metadata in pack", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ParseID("test:entry"),
			Kind: "test.kind",
			Data: payload.New(map[string]any{"value": 42}),
		}

		var buf bytes.Buffer
		err := packer.PackEntries(nil, []registry.Entry{entry}, &buf)
		require.NoError(t, err)

		reader, err := NewReader(bytes.NewReader(buf.Bytes()), transcoder)
		require.NoError(t, err)
		unpacked, err := reader.GetEntries()
		require.NoError(t, err)
		assert.Len(t, unpacked, 1)
	})
}
