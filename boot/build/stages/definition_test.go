package stages

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

func TestFindDefinition_Found(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "worker"),
			Kind: "process.lua",
		},
		{
			ID:   registry.NewID("app", "definition"),
			Kind: registry.NamespaceDefinition,
			Data: payload.New(map[string]any{
				"module": "app",
				"readme": "# App Module",
			}),
		},
		{
			ID:   registry.NewID("app", "handler"),
			Kind: "function.lua",
		},
	}

	def := FindDefinition(entries)
	require.NotNil(t, def)
	assert.Equal(t, "app", def.ID.NS)
	assert.Equal(t, "definition", def.ID.Name)
	assert.Equal(t, registry.NamespaceDefinition, def.Kind)
}

func TestFindDefinition_NotFound(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "worker"),
			Kind: "process.lua",
		},
		{
			ID:   registry.NewID("app", "handler"),
			Kind: "function.lua",
		},
	}

	def := FindDefinition(entries)
	assert.Nil(t, def)
}

func TestFindDefinition_EmptyEntries(t *testing.T) {
	var entries []registry.Entry
	def := FindDefinition(entries)
	assert.Nil(t, def)
}

func TestFindDefinitions_Multiple(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "definition"),
			Kind: registry.NamespaceDefinition,
		},
		{
			ID:   registry.NewID("app", "worker"),
			Kind: "process.lua",
		},
		{
			ID:   registry.NewID("lib", "definition"),
			Kind: registry.NamespaceDefinition,
		},
	}

	defs := FindDefinitions(entries)
	assert.Len(t, defs, 2)
	assert.Equal(t, "app", defs[0].ID.NS)
	assert.Equal(t, "lib", defs[1].ID.NS)
}

func TestFindDefinitions_None(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "worker"),
			Kind: "process.lua",
		},
	}

	defs := FindDefinitions(entries)
	assert.Len(t, defs, 0)
}

func TestValidateDefinitionForPublish_Valid(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "worker"),
			Kind: "process.lua",
		},
		{
			ID:   registry.NewID("app", "definition"),
			Kind: registry.NamespaceDefinition,
			Data: payload.New(map[string]any{
				"module": "app",
				"readme": "# App Module\n\nDescription here.",
			}),
			Meta: map[string]any{
				"license":    "MIT",
				"repository": "github.com/example/app",
			},
		},
	}

	err := ValidateDefinitionForPublish(entries)
	assert.NoError(t, err)
}

func TestValidateDefinitionForPublish_Missing(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "worker"),
			Kind: "process.lua",
		},
		{
			ID:   registry.NewID("app", "handler"),
			Kind: "function.lua",
		},
	}

	err := ValidateDefinitionForPublish(entries)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoDefinition)
}

func TestValidateDefinitionForPublish_Multiple(t *testing.T) {
	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "definition"),
			Kind: registry.NamespaceDefinition,
		},
		{
			ID:   registry.NewID("lib", "definition"),
			Kind: registry.NamespaceDefinition,
		},
	}

	err := ValidateDefinitionForPublish(entries)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one ns.definition entry required")
	assert.Contains(t, err.Error(), "found 2")
}

func TestValidateDefinitionForPublish_EmptyEntries(t *testing.T) {
	var entries []registry.Entry
	err := ValidateDefinitionForPublish(entries)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoDefinition)
}

func TestDecodeDefinition(t *testing.T) {
	ctx, transcoder := setupTestContext()

	entry := registry.Entry{
		ID:   registry.NewID("app", "definition"),
		Kind: registry.NamespaceDefinition,
		Data: payload.New(map[string]any{
			"module": "my-module",
			"readme": "# My Module\n\nThis is the readme content.",
		}),
		Meta: map[string]any{
			"license": "MIT",
		},
	}

	def, err := DecodeDefinition(ctx, transcoder, entry)
	require.NoError(t, err)
	assert.Equal(t, "my-module", def.Module)
	assert.Equal(t, "# My Module\n\nThis is the readme content.", def.Readme)
}

func TestDecodeDefinition_FileReference(t *testing.T) {
	// File references are resolved during loading, here we verify decode works
	ctx, transcoder := setupTestContext()

	entry := registry.Entry{
		ID:   registry.NewID("app", "definition"),
		Kind: registry.NamespaceDefinition,
		Data: payload.New(map[string]any{
			"module": "my-module",
			"readme": "file://README.md",
		}),
	}

	def, err := DecodeDefinition(ctx, transcoder, entry)
	require.NoError(t, err)
	assert.Equal(t, "my-module", def.Module)
	assert.Equal(t, "file://README.md", def.Readme)
}
