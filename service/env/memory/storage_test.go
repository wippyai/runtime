// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/env"
)

func TestNewStorage(t *testing.T) {
	t.Run("empty defaults", func(t *testing.T) {
		storage := NewStorage(nil)
		assert.NotNil(t, storage)

		values, err := storage.List(context.Background())
		require.NoError(t, err)
		assert.Empty(t, values)
	})

	t.Run("with defaults", func(t *testing.T) {
		defaults := map[string]string{
			"KEY1": "value1",
			"KEY2": "value2",
		}
		storage := NewStorage(defaults)
		assert.NotNil(t, storage)

		values, err := storage.List(context.Background())
		require.NoError(t, err)
		assert.Len(t, values, 2)
		assert.Equal(t, "value1", values["KEY1"])
		assert.Equal(t, "value2", values["KEY2"])
	})
}

var _ env.Storage = (*Storage)(nil)

func TestStorage_Get(t *testing.T) {
	storage := NewStorage(map[string]string{"KEY1": "value1"})

	t.Run("existing key", func(t *testing.T) {
		value, err := storage.Get(context.Background(), "KEY1")
		require.NoError(t, err)
		assert.Equal(t, "value1", value)
	})

	t.Run("non-existent key", func(t *testing.T) {
		_, err := storage.Get(context.Background(), "NONEXISTENT")
		assert.ErrorIs(t, err, env.ErrVariableNotFound)
	})
}

func TestStorage_Set(t *testing.T) {
	storage := NewStorage(nil)

	t.Run("new key", func(t *testing.T) {
		err := storage.Set(context.Background(), "KEY1", "value1")
		require.NoError(t, err)

		value, err := storage.Get(context.Background(), "KEY1")
		require.NoError(t, err)
		assert.Equal(t, "value1", value)
	})

	t.Run("update existing key", func(t *testing.T) {
		err := storage.Set(context.Background(), "KEY1", "updated")
		require.NoError(t, err)

		value, err := storage.Get(context.Background(), "KEY1")
		require.NoError(t, err)
		assert.Equal(t, "updated", value)
	})

	t.Run("empty value", func(t *testing.T) {
		err := storage.Set(context.Background(), "EMPTY", "")
		require.NoError(t, err)

		value, err := storage.Get(context.Background(), "EMPTY")
		require.NoError(t, err)
		assert.Equal(t, "", value)
	})
}

func TestStorage_Delete(t *testing.T) {
	storage := NewStorage(map[string]string{"KEY1": "value1"})

	err := storage.Delete(context.Background(), "KEY1")
	require.NoError(t, err)

	_, err = storage.Get(context.Background(), "KEY1")
	assert.ErrorIs(t, err, env.ErrVariableNotFound)

	err = storage.Delete(context.Background(), "NONEXISTENT")
	require.NoError(t, err)
}

func TestStorage_List(t *testing.T) {
	storage := NewStorage(nil)

	_ = storage.Set(context.Background(), "KEY1", "value1")
	_ = storage.Set(context.Background(), "KEY2", "value2")
	_ = storage.Set(context.Background(), "KEY3", "value3")

	values, err := storage.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, values, 3)
	assert.Equal(t, "value1", values["KEY1"])
	assert.Equal(t, "value2", values["KEY2"])
	assert.Equal(t, "value3", values["KEY3"])
}

func TestStorage_Concurrent(t *testing.T) {
	storage := NewStorage(nil)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		key := "KEY"

		go func() {
			defer wg.Done()
			_ = storage.Set(ctx, key, "value")
		}()

		go func() {
			defer wg.Done()
			_, _ = storage.Get(ctx, key)
		}()

		go func() {
			defer wg.Done()
			_, _ = storage.List(ctx)
		}()
	}
	assert.NotPanics(t, func() {
		wg.Wait()
	})
}

// Benchmarks

func BenchmarkStorage_Get(b *testing.B) {
	storage := NewStorage(map[string]string{"KEY": "value"})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.Get(ctx, "KEY")
	}
}

func BenchmarkStorage_Set(b *testing.B) {
	storage := NewStorage(nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = storage.Set(ctx, "KEY", "value")
	}
}

func BenchmarkStorage_List(b *testing.B) {
	data := make(map[string]string)
	for i := 0; i < 100; i++ {
		data["KEY"+string(rune('A'+i))] = "value"
	}
	storage := NewStorage(data)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.List(ctx)
	}
}

func BenchmarkStorage_GetParallel(b *testing.B) {
	storage := NewStorage(map[string]string{"KEY": "value"})
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = storage.Get(ctx, "KEY")
		}
	})
}

func BenchmarkStorage_SetParallel(b *testing.B) {
	storage := NewStorage(nil)
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = storage.Set(ctx, "KEY", "value")
		}
	})
}
