package embed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/pack"
)

func TestRegistry_NewRegistry(t *testing.T) {
	r := NewRegistry()
	require.NotNil(t, r)
	assert.NotNil(t, r.readers)
}

func TestRegistry_Register(t *testing.T) {
	t.Run("successful registration", func(t *testing.T) {
		r := NewRegistry()
		reader := &pack.Reader{}

		err := r.Register("test/pack", reader)
		require.NoError(t, err)

		r.mu.RLock()
		stored := r.readers["test/pack"]
		r.mu.RUnlock()
		assert.Equal(t, reader, stored)
	})

	t.Run("empty pack path", func(t *testing.T) {
		r := NewRegistry()
		reader := &pack.Reader{}

		err := r.Register("", reader)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "packPath cannot be empty")
	})

	t.Run("nil reader", func(t *testing.T) {
		r := NewRegistry()

		err := r.Register("test/pack", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reader cannot be nil")
	})

	t.Run("overwrite existing", func(t *testing.T) {
		r := NewRegistry()
		reader1 := &pack.Reader{}
		reader2 := &pack.Reader{}

		err := r.Register("test/pack", reader1)
		require.NoError(t, err)

		err = r.Register("test/pack", reader2)
		require.NoError(t, err)

		r.mu.RLock()
		stored := r.readers["test/pack"]
		r.mu.RUnlock()
		assert.Equal(t, reader2, stored)
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
		reader1 := &pack.Reader{}
		reader2 := &pack.Reader{}

		err := r.Register("pack1", reader1)
		require.NoError(t, err)
		err = r.Register("pack2", reader2)
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.readers, 2)
		r.mu.RUnlock()

		err = r.Close()
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.readers, 0)
		r.mu.RUnlock()
	})

	t.Run("idempotent", func(t *testing.T) {
		r := NewRegistry()
		reader := &pack.Reader{}

		err := r.Register("pack", reader)
		require.NoError(t, err)

		err = r.Close()
		require.NoError(t, err)

		err = r.Close()
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.readers, 0)
		r.mu.RUnlock()
	})
}
