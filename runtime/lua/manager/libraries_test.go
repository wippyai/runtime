package manager

import (
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewLibraries(t *testing.T) {
	logger := zap.NewNop()

	t.Run("creates new instance", func(t *testing.T) {
		libs := NewLibraries(logger)
		assert.NotNil(t, libs)
		assert.NotNil(t, libs.libraries)
		assert.Empty(t, libs.libraries)
	})
}

func TestLibraries_Add(t *testing.T) {
	logger := zap.NewNop()
	libs := NewLibraries(logger)

	t.Run("adds new library successfully", func(t *testing.T) {
		cfg := &api.LibraryConfig{
			Source: "return {test = function() return 'hello' end}",
			Meta:   registry.Metadata{"name": "test1"},
		}

		err := libs.Add("test1", cfg)
		require.NoError(t, err)

		// Verify library was stored
		stored, err := libs.Get("test1")
		assert.NoError(t, err)
		assert.Equal(t, cfg, stored)
	})

	t.Run("fails adding duplicate library", func(t *testing.T) {
		cfg := &api.LibraryConfig{
			Source: "return {test = function() return 'hello' end}",
			Meta:   registry.Metadata{"name": "test1"},
		}

		err := libs.Add("test1", cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("adds multiple libraries", func(t *testing.T) {
		cfg2 := &api.LibraryConfig{
			Source: "return {test2 = function() return 'world' end}",
			Meta:   registry.Metadata{"name": "test2"},
		}
		cfg3 := &api.LibraryConfig{
			Source: "return {test3 = function() return '!' end}",
			Meta:   registry.Metadata{"name": "test3"},
		}

		err := libs.Add("test2", cfg2)
		require.NoError(t, err)
		err = libs.Add("test3", cfg3)
		require.NoError(t, err)

		// Verify both libraries exist
		assert.True(t, libs.Has("test2"))
		assert.True(t, libs.Has("test3"))
	})
}

func TestLibraries_Update(t *testing.T) {
	logger := zap.NewNop()
	libs := NewLibraries(logger)

	// First add a library
	initialCfg := &api.LibraryConfig{
		Source: "return {test = function() return 'hello' end}",
		Meta:   registry.Metadata{"name": "test"},
	}
	err := libs.Add("test", initialCfg)
	require.NoError(t, err)

	t.Run("updates existing library", func(t *testing.T) {
		updatedCfg := &api.LibraryConfig{
			Source: "return {test = function() return 'updated' end}",
			Meta:   registry.Metadata{"name": "test", "version": "2"},
		}

		err := libs.Update("test", updatedCfg)
		require.NoError(t, err)

		// Verify library was updated
		stored, err := libs.Get("test")
		assert.NoError(t, err)
		assert.Equal(t, updatedCfg, stored)
	})

	t.Run("fails updating non-existent library", func(t *testing.T) {
		cfg := &api.LibraryConfig{
			Source: "return {}",
			Meta:   registry.Metadata{"name": "non-existent"},
		}

		err := libs.Update("non-existent", cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestLibraries_Delete(t *testing.T) {
	logger := zap.NewNop()
	libs := NewLibraries(logger)

	// First add a library
	cfg := &api.LibraryConfig{
		Source: "return {test = function() return 'hello' end}",
		Meta:   registry.Metadata{"name": "test"},
	}
	err := libs.Add("test", cfg)
	require.NoError(t, err)

	t.Run("deletes existing library", func(t *testing.T) {
		err := libs.Delete("test")
		require.NoError(t, err)

		// Verify library was deleted
		assert.False(t, libs.Has("test"))
	})

	t.Run("fails deleting non-existent library", func(t *testing.T) {
		err := libs.Delete("non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestLibraries_Clone(t *testing.T) {
	logger := zap.NewNop()
	libs := NewLibraries(logger)

	// Add a test library
	cfg := &api.LibraryConfig{
		Source: "return {test = function() return 'hello' end}",
		Meta:   registry.Metadata{"name": "test"},
	}
	err := libs.Add("test", cfg)
	require.NoError(t, err)

	t.Run("creates exact copy of libraries", func(t *testing.T) {
		cloned := libs.Clone()

		// Verify cloned instance has same libraries
		assert.Equal(t, len(libs.libraries), len(cloned.libraries))

		original, err := libs.Get("test")
		assert.NoError(t, err)

		clonedCfg, err := cloned.Get("test")
		assert.NoError(t, err)
		assert.Equal(t, original, clonedCfg)
	})

	t.Run("modifications don't affect original", func(t *testing.T) {
		cloned := libs.Clone()

		// Modify cloned instance
		err := cloned.Delete("test")
		require.NoError(t, err)

		// Verify original is unchanged
		_, err = libs.Get("test")
		assert.NoError(t, err)

		// Verify cloned instance was modified
		_, err = cloned.Get("test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestLibraries_Get(t *testing.T) {
	logger := zap.NewNop()
	libs := NewLibraries(logger)

	cfg := &api.LibraryConfig{
		Source: "return {test = function() return 'hello' end}",
		Meta:   registry.Metadata{"name": "test"},
	}

	t.Run("gets existing library", func(t *testing.T) {
		err := libs.Add("test", cfg)
		require.NoError(t, err)

		stored, err := libs.Get("test")
		assert.NoError(t, err)
		assert.Equal(t, cfg, stored)
	})

	t.Run("returns error for non-existent library", func(t *testing.T) {
		stored, err := libs.Get("non-existent")
		assert.Error(t, err)
		assert.Nil(t, stored)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestLibraries_Has(t *testing.T) {
	logger := zap.NewNop()
	libs := NewLibraries(logger)

	cfg := &api.LibraryConfig{
		Source: "return {test = function() return 'hello' end}",
		Meta:   registry.Metadata{"name": "test"},
	}

	t.Run("returns true for existing library", func(t *testing.T) {
		err := libs.Add("test", cfg)
		require.NoError(t, err)

		exists := libs.Has("test")
		assert.True(t, exists)
	})

	t.Run("returns false for non-existent library", func(t *testing.T) {
		exists := libs.Has("non-existent")
		assert.False(t, exists)
	})
}
