package pack

import (
	"bytes"
	"crypto/sha256"
	"fmt"
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

		err := os.WriteFile(badPath, buf.Bytes(), 0600)
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

		err := os.WriteFile(badPath, buf.Bytes(), 0600)
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

		err := os.WriteFile(tinyPath, []byte("tiny"), 0600)
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

		err = os.WriteFile(packPath, data, 0600)
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

		err := os.WriteFile(badPath, buf.Bytes(), 0600)
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

func TestUnpackBytes(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	packer := New(transcoder)

	t.Run("successful unpack from bytes", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry1"),
				Kind: "process.lua",
				Meta: registry.Metadata{"version": "1.0"},
				Data: payload.New(map[string]any{"code": "return 42"}),
			},
		}

		var buf bytes.Buffer
		err := packer.Pack(entries, &buf, testMetadata(len(entries)))
		require.NoError(t, err)

		packedBytes := buf.Bytes()
		unpacked, meta, err := packer.UnpackBytes(packedBytes)
		require.NoError(t, err)
		require.NotNil(t, meta)

		assert.Len(t, unpacked, 1)
		assert.Equal(t, entries[0].ID, unpacked[0].ID)
		assert.Equal(t, entries[0].Kind, unpacked[0].Kind)
	})

	t.Run("data too small", func(t *testing.T) {
		_, _, err := packer.UnpackBytes([]byte("tiny"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "data too small")
	})

	t.Run("invalid magic header", func(t *testing.T) {
		badData := make([]byte, 100)
		copy(badData, "BADMAGIC")
		_, _, err := packer.UnpackBytes(badData)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid magic header")
	})

	t.Run("unsupported version", func(t *testing.T) {
		badData := make([]byte, 100)
		copy(badData, magic)
		badData[len(magic)] = 99
		_, _, err := packer.UnpackBytes(badData)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported version")
	})

	t.Run("invalid compressed data", func(t *testing.T) {
		badData := make([]byte, len(magic)+1+hashLen+20)
		copy(badData, magic)
		badData[len(magic)] = version
		copy(badData[len(magic)+1:], make([]byte, hashLen))
		copy(badData[len(magic)+1+hashLen:], "not valid zstd")

		_, _, err := packer.UnpackBytes(badData)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "zstd")
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
		err := packer.Pack(entries, &buf, testMetadata(len(entries)))
		require.NoError(t, err)

		packedBytes := buf.Bytes()
		packedBytes[len(magic)+1+hashLen+5] ^= 0xFF

		_, _, err = packer.UnpackBytes(packedBytes)
		assert.Error(t, err)
	})

	t.Run("empty entries pack and unpack", func(t *testing.T) {
		entries := []registry.Entry{}

		var buf bytes.Buffer
		err := packer.Pack(entries, &buf, testMetadata(0))
		require.NoError(t, err)

		unpacked, meta, err := packer.UnpackBytes(buf.Bytes())
		require.NoError(t, err)
		assert.Empty(t, unpacked)
		assert.NotNil(t, meta)
	})
}

func TestVerifyChecksum(t *testing.T) {
	t.Run("valid checksum passes", func(t *testing.T) {
		data := []byte("test data for checksum")
		hash := sha256.Sum256(data)
		expected := fmt.Sprintf("%x", hash)

		result := verifyChecksum(data, expected)
		assert.True(t, result)
	})

	t.Run("invalid checksum fails", func(t *testing.T) {
		data := []byte("test data")
		wrongChecksum := "0000000000000000000000000000000000000000000000000000000000000000"

		result := verifyChecksum(data, wrongChecksum)
		assert.False(t, result)
	})

	t.Run("empty data has valid checksum", func(t *testing.T) {
		data := []byte{}
		hash := sha256.Sum256(data)
		expected := fmt.Sprintf("%x", hash)

		result := verifyChecksum(data, expected)
		assert.True(t, result)
	})

	t.Run("different data different checksums", func(t *testing.T) {
		data1 := []byte("data one")
		data2 := []byte("data two")

		hash1 := sha256.Sum256(data1)
		checksum1 := fmt.Sprintf("%x", hash1)

		result := verifyChecksum(data2, checksum1)
		assert.False(t, result)
	})

	t.Run("case sensitive checksum", func(t *testing.T) {
		data := []byte("test")
		hash := sha256.Sum256(data)
		checksumLower := fmt.Sprintf("%x", hash)
		checksumUpper := fmt.Sprintf("%X", hash)

		resultLower := verifyChecksum(data, checksumLower)
		resultUpper := verifyChecksum(data, checksumUpper)

		assert.True(t, resultLower)
		assert.False(t, resultUpper)
	})
}

func TestNormalizePayload(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	packer := New(transcoder)

	t.Run("normalizes golang payload", func(t *testing.T) {
		golangData := payload.New(map[string]any{"key": "value"})

		normalized, err := packer.normalizePayload(golangData)
		require.NoError(t, err)
		assert.NotNil(t, normalized)
		assert.Equal(t, payload.Golang, normalized.Format())
	})

	t.Run("Error format unchanged", func(t *testing.T) {
		errorData := payload.NewPayload([]byte("error message"), payload.Error)

		normalized, err := packer.normalizePayload(errorData)
		require.NoError(t, err)
		assert.Equal(t, payload.Error, normalized.Format())
	})

	t.Run("String format unchanged", func(t *testing.T) {
		stringData := payload.NewPayload([]byte("test string"), payload.String)

		normalized, err := packer.normalizePayload(stringData)
		require.NoError(t, err)
		assert.Equal(t, payload.String, normalized.Format())
	})

	t.Run("Bytes format unchanged", func(t *testing.T) {
		bytesData := payload.NewPayload([]byte{0x01, 0x02, 0x03}, payload.Bytes)

		normalized, err := packer.normalizePayload(bytesData)
		require.NoError(t, err)
		assert.Equal(t, payload.Bytes, normalized.Format())
	})

	t.Run("handles nil payload", func(t *testing.T) {
		normalized, err := packer.normalizePayload(nil)
		require.NoError(t, err)
		assert.Nil(t, normalized)
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

		golangPayload := payload.New(complexData)
		normalized, err := packer.normalizePayload(golangPayload)
		require.NoError(t, err)
		assert.Equal(t, payload.Golang, normalized.Format())

		data := normalized.Data()
		assert.NotNil(t, data)
	})
}

func TestPackEdgeCases(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	packer := New(transcoder)

	t.Run("large number of entries", func(t *testing.T) {
		entries := make([]registry.Entry, 1000)
		for i := 0; i < 1000; i++ {
			entries[i] = registry.Entry{
				ID:   registry.ParseID(fmt.Sprintf("test:entry%d", i)),
				Kind: "test.kind",
				Data: payload.New(map[string]any{"index": i}),
			}
		}

		var buf bytes.Buffer
		err := packer.Pack(entries, &buf, testMetadata(len(entries)))
		require.NoError(t, err)

		unpacked, _, err := packer.UnpackBytes(buf.Bytes())
		require.NoError(t, err)
		assert.Len(t, unpacked, 1000)
	})

	t.Run("entries with empty metadata", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ParseID("test:entry"),
			Kind: "test.kind",
			Meta: registry.Metadata{},
			Data: payload.New(map[string]any{"value": 42}),
		}

		var buf bytes.Buffer
		err := packer.Pack([]registry.Entry{entry}, &buf, registry.Metadata{})
		require.NoError(t, err)

		unpacked, meta, err := packer.UnpackBytes(buf.Bytes())
		require.NoError(t, err)
		assert.Len(t, unpacked, 1)
		assert.NotNil(t, meta)
	})

	t.Run("nil metadata in pack", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ParseID("test:entry"),
			Kind: "test.kind",
			Data: payload.New(map[string]any{"value": 42}),
		}

		var buf bytes.Buffer
		err := packer.Pack([]registry.Entry{entry}, &buf, nil)
		require.NoError(t, err)

		unpacked, _, err := packer.UnpackBytes(buf.Bytes())
		require.NoError(t, err)
		assert.Len(t, unpacked, 1)
	})
}
