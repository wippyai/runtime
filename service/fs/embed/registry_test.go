package embed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

func TestRegistry_NewRegistry(t *testing.T) {
	r := NewRegistry()
	require.NotNil(t, r)
	assert.NotNil(t, r.readers)
}

func TestRegistry_Register(t *testing.T) {
	t.Run("empty pack path", func(t *testing.T) {
		r := NewRegistry()

		err := r.Register("", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "packPath cannot be empty")
	})

	t.Run("nil reader", func(t *testing.T) {
		r := NewRegistry()

		err := r.Register("test/pack", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reader cannot be nil")
	})
}

func TestRegistry_GetFS(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		r := NewRegistry()

		_, err := r.GetFS(registry.NewID("test", "notfound"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestRegistry_Close(t *testing.T) {
	t.Run("clears all readers", func(t *testing.T) {
		r := NewRegistry()

		r.mu.RLock()
		assert.Len(t, r.readers, 0)
		r.mu.RUnlock()

		err := r.Close()
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.readers, 0)
		r.mu.RUnlock()
	})

	t.Run("idempotent", func(t *testing.T) {
		r := NewRegistry()

		err := r.Close()
		require.NoError(t, err)

		err = r.Close()
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.readers, 0)
		r.mu.RUnlock()
	})
}
