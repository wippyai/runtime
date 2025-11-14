package pack

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	systempayload "github.com/ponyruntime/pony/system/payload"
	"github.com/stretchr/testify/require"
)

func TestDeterministicEncoding(t *testing.T) {
	transcoder := systempayload.NewTranscoder()
	packer := New(transcoder)

	tmpDir := t.TempDir()

	t.Run("different map construction order produces identical files", func(t *testing.T) {
		// Create two maps with same content but different construction order
		map1 := map[string]any{}
		map1["z"] = "last"
		map1["a"] = "first"
		map1["m"] = "middle"

		map2 := map[string]any{}
		map2["a"] = "first"
		map2["m"] = "middle"
		map2["z"] = "last"

		entries1 := []registry.Entry{
			{
				ID:   registry.ParseID("test:order"),
				Kind: "test.kind",
				Meta: registry.Metadata{"z": "last", "a": "first", "m": "middle"},
				Data: payload.New(map1),
			},
		}

		entries2 := []registry.Entry{
			{
				ID:   registry.ParseID("test:order"),
				Kind: "test.kind",
				Meta: registry.Metadata{"a": "first", "m": "middle", "z": "last"},
				Data: payload.New(map2),
			},
		}

		path1 := filepath.Join(tmpDir, "pack1.pack")
		path2 := filepath.Join(tmpDir, "pack2.pack")

		err := packer.Pack(entries1, path1)
		require.NoError(t, err)

		err = packer.Pack(entries2, path2)
		require.NoError(t, err)

		data1, err := os.ReadFile(path1)
		require.NoError(t, err)

		data2, err := os.ReadFile(path2)
		require.NoError(t, err)

		require.True(t, bytes.Equal(data1, data2), "Binary files should be identical despite different map construction order")
	})

	t.Run("multiple packs produce identical files", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("test:repeat"),
				Kind: "test.kind",
				Meta: registry.Metadata{"key": "value"},
				Data: payload.New(map[string]any{"field": "data"}),
			},
		}

		var files [][]byte
		for i := 0; i < 5; i++ {
			path := filepath.Join(tmpDir, "repeat"+string(rune('0'+i))+".pack")
			err := packer.Pack(entries, path)
			require.NoError(t, err)

			data, err := os.ReadFile(path)
			require.NoError(t, err)
			files = append(files, data)
		}

		// Check all files are identical
		for i := 1; i < len(files); i++ {
			require.True(t, bytes.Equal(files[0], files[i]), "All packed files should be bit-identical")
		}
	})

	t.Run("nested maps produce deterministic output", func(t *testing.T) {
		// Create entries with deeply nested maps
		entries1 := []registry.Entry{
			{
				ID:   registry.ParseID("test:nested"),
				Kind: "test.kind",
				Meta: registry.Metadata{},
				Data: payload.New(map[string]any{
					"outer": map[string]any{
						"z": "last",
						"a": "first",
						"inner": map[string]any{
							"c": 3,
							"a": 1,
							"b": 2,
						},
					},
				}),
			},
		}

		entries2 := []registry.Entry{
			{
				ID:   registry.ParseID("test:nested"),
				Kind: "test.kind",
				Meta: registry.Metadata{},
				Data: payload.New(map[string]any{
					"outer": map[string]any{
						"a": "first",
						"inner": map[string]any{
							"a": 1,
							"b": 2,
							"c": 3,
						},
						"z": "last",
					},
				}),
			},
		}

		path1 := filepath.Join(tmpDir, "nested1.pack")
		path2 := filepath.Join(tmpDir, "nested2.pack")

		err := packer.Pack(entries1, path1)
		require.NoError(t, err)

		err = packer.Pack(entries2, path2)
		require.NoError(t, err)

		data1, err := os.ReadFile(path1)
		require.NoError(t, err)

		data2, err := os.ReadFile(path2)
		require.NoError(t, err)

		require.True(t, bytes.Equal(data1, data2), "Nested maps should produce identical output regardless of key order")
	})
}
