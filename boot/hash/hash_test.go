package hash

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	systempayload "github.com/wippyai/runtime/system/payload"
)

func TestHashEntries(t *testing.T) {
	hasher := New(systempayload.NewTranscoder())

	t.Run("empty entries", func(t *testing.T) {
		hash, err := hasher.Hash(nil)
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
		assert.Len(t, hash, 64) // SHA-256 hex

		hash2, err := hasher.Hash([]registry.Entry{})
		require.NoError(t, err)
		assert.Equal(t, hash, hash2)
	})

	t.Run("single entry", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry1"),
				Kind: "test.kind",
				Meta: attrs.Bag{"key": "value"},
				Data: payload.New(map[string]any{"field": "data"}),
			},
		}

		hash, err := hasher.Hash(entries)
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
		assert.Len(t, hash, 64)
	})

	t.Run("same content produces same hash", func(t *testing.T) {
		entries1 := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry1"),
				Kind: "test.kind",
				Meta: attrs.Bag{"key": "value"},
				Data: payload.New(map[string]any{"field": "data"}),
			},
		}

		entries2 := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry1"),
				Kind: "test.kind",
				Meta: attrs.Bag{"key": "value"},
				Data: payload.New(map[string]any{"field": "data"}),
			},
		}

		hash1, err := hasher.Hash(entries1)
		require.NoError(t, err)

		hash2, err := hasher.Hash(entries2)
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("entry order does not matter", func(t *testing.T) {
		entries1 := []registry.Entry{
			{
				ID:   registry.ParseID("test:b"),
				Kind: "test.kind",
				Meta: attrs.Bag{},
				Data: payload.New(map[string]any{"val": "2"}),
			},
			{
				ID:   registry.ParseID("test:a"),
				Kind: "test.kind",
				Meta: attrs.Bag{},
				Data: payload.New(map[string]any{"val": "1"}),
			},
			{
				ID:   registry.ParseID("test:c"),
				Kind: "test.kind",
				Meta: attrs.Bag{},
				Data: payload.New(map[string]any{"val": "3"}),
			},
		}

		entries2 := []registry.Entry{
			{
				ID:   registry.ParseID("test:a"),
				Kind: "test.kind",
				Meta: attrs.Bag{},
				Data: payload.New(map[string]any{"val": "1"}),
			},
			{
				ID:   registry.ParseID("test:c"),
				Kind: "test.kind",
				Meta: attrs.Bag{},
				Data: payload.New(map[string]any{"val": "3"}),
			},
			{
				ID:   registry.ParseID("test:b"),
				Kind: "test.kind",
				Meta: attrs.Bag{},
				Data: payload.New(map[string]any{"val": "2"}),
			},
		}

		hash1, err := hasher.Hash(entries1)
		require.NoError(t, err)

		hash2, err := hasher.Hash(entries2)
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("field order does not matter", func(t *testing.T) {
		entries1 := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: attrs.Bag{"a": "1", "b": "2", "c": "3"},
				Data: payload.New(map[string]any{"x": "1", "y": "2", "z": "3"}),
			},
		}

		entries2 := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: attrs.Bag{"c": "3", "a": "1", "b": "2"},
				Data: payload.New(map[string]any{"z": "3", "x": "1", "y": "2"}),
			},
		}

		hash1, err := hasher.Hash(entries1)
		require.NoError(t, err)

		hash2, err := hasher.Hash(entries2)
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("different content produces different hash", func(t *testing.T) {
		entries1 := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: attrs.Bag{},
				Data: payload.New(map[string]any{"val": "1"}),
			},
		}

		entries2 := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: attrs.Bag{},
				Data: payload.New(map[string]any{"val": "2"}),
			},
		}

		hash1, err := hasher.Hash(entries1)
		require.NoError(t, err)

		hash2, err := hasher.Hash(entries2)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("nested maps sorted", func(t *testing.T) {
		entries1 := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: attrs.Bag{},
				Data: payload.New(map[string]any{
					"outer": map[string]any{
						"b": "2",
						"a": "1",
						"c": "3",
					},
				}),
			},
		}

		entries2 := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: attrs.Bag{},
				Data: payload.New(map[string]any{
					"outer": map[string]any{
						"c": "3",
						"a": "1",
						"b": "2",
					},
				}),
			},
		}

		hash1, err := hasher.Hash(entries1)
		require.NoError(t, err)

		hash2, err := hasher.Hash(entries2)
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("array order matters", func(t *testing.T) {
		entries1 := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: attrs.Bag{},
				Data: payload.New([]any{"a", "b", "c"}),
			},
		}

		entries2 := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: attrs.Bag{},
				Data: payload.New([]any{"c", "b", "a"}),
			},
		}

		hash1, err := hasher.Hash(entries1)
		require.NoError(t, err)

		hash2, err := hasher.Hash(entries2)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("nil data", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: attrs.Bag{"key": "value"},
				Data: nil,
			},
		}

		hash, err := hasher.Hash(entries)
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
	})

	t.Run("empty metadata", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("test:entry"),
				Kind: "test.kind",
				Meta: nil,
				Data: payload.New(map[string]any{"field": "data"}),
			},
		}

		hash, err := hasher.Hash(entries)
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
	})
}

func TestDependencyEntries(t *testing.T) {
	hasher := New(systempayload.NewTranscoder())

	t.Run("ns.dependency entry", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("ns:my-dependency"),
				Kind: registry.NamespaceDependencyKind,
				Meta: attrs.Bag{"parent": "some-parent"},
				Data: payload.New(map[string]any{
					"component": "wippy/actor",
					"parameters": []any{
						map[string]any{
							"name":  "version",
							"value": "1.0.0",
						},
					},
				}),
			},
		}

		hash, err := hasher.Hash(entries)
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
		assert.Len(t, hash, 64)
	})

	t.Run("ns.requirement entry", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("ns:my-requirement"),
				Kind: registry.NamespaceRequirementKind,
				Meta: attrs.Bag{"parent": "ns:my-dependency"},
				Data: payload.New(map[string]any{
					"targets": []any{
						map[string]any{
							"entry": "config:database",
							"path":  ".data.host",
						},
					},
					"default": "localhost",
				}),
			},
		}

		hash, err := hasher.Hash(entries)
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
		assert.Len(t, hash, 64)
	})

	t.Run("multiple dependency entries", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ParseID("ns:dep-a"),
				Kind: registry.NamespaceDependencyKind,
				Meta: attrs.Bag{},
				Data: payload.New(map[string]any{
					"component":  "wippy/actor",
					"parameters": []any{},
				}),
			},
			{
				ID:   registry.ParseID("ns:dep-b"),
				Kind: registry.NamespaceDependencyKind,
				Meta: attrs.Bag{},
				Data: payload.New(map[string]any{
					"component":  "wippy/logger",
					"parameters": []any{},
				}),
			},
		}

		hash, err := hasher.Hash(entries)
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
	})
}

func TestNormalize(t *testing.T) {
	t.Run("primitives unchanged", func(t *testing.T) {
		assert.Equal(t, "string", normalize("string"))
		assert.Equal(t, 42, normalize(42))
		assert.Equal(t, 3.14, normalize(3.14))
		assert.Equal(t, true, normalize(true))
		assert.Nil(t, normalize(nil))
	})

	t.Run("maps sorted", func(t *testing.T) {
		input := map[string]any{
			"z": "last",
			"a": "first",
			"m": "middle",
		}

		result := normalize(input)
		assert.NotNil(t, result)

		pairs, ok := result.([]kv)
		require.True(t, ok)
		require.Len(t, pairs, 3)

		assert.Equal(t, "a", pairs[0].K)
		assert.Equal(t, "m", pairs[1].K)
		assert.Equal(t, "z", pairs[2].K)
	})

	t.Run("nested structures", func(t *testing.T) {
		input := map[string]any{
			"outer": map[string]any{
				"z": "last",
				"a": "first",
			},
			"list": []any{1, 2, 3},
		}

		result := normalize(input)
		assert.NotNil(t, result)
	})

	t.Run("nil slice", func(t *testing.T) {
		var input []any
		result := normalize(input)
		assert.Nil(t, result)
	})

	t.Run("empty slice", func(t *testing.T) {
		var input []any
		result := normalize(input)
		assert.Nil(t, result)
	})

	t.Run("nil map", func(t *testing.T) {
		var input map[string]any
		result := normalize(input)
		assert.Nil(t, result)
	})

	t.Run("empty map", func(t *testing.T) {
		input := map[string]any{}
		result := normalize(input)
		assert.Nil(t, result)
	})
}
