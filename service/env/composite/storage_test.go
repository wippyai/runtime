// SPDX-License-Identifier: MPL-2.0

package composite

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/service/env/memory"
	envos "github.com/wippyai/runtime/service/env/os"
)

var _ env.Storage = (*Storage)(nil)

func TestNewStorage(t *testing.T) {
	t.Run("with storages", func(t *testing.T) {
		memStorage := memory.NewStorage(nil)
		storage, err := NewStorage([]env.Storage{memStorage})
		require.NoError(t, err)
		assert.NotNil(t, storage)
	})

	t.Run("empty storages error", func(t *testing.T) {
		_, err := NewStorage([]env.Storage{})
		assert.ErrorIs(t, err, env.ErrNoStorages)
	})

	t.Run("nil storages error", func(t *testing.T) {
		_, err := NewStorage(nil)
		assert.ErrorIs(t, err, env.ErrNoStorages)
	})
}

func TestStorage_Get(t *testing.T) {
	t.Run("found in first storage", func(t *testing.T) {
		mem1 := memory.NewStorage(map[string]string{"KEY": "value1"})
		mem2 := memory.NewStorage(map[string]string{"KEY": "value2"})

		storage, err := NewStorage([]env.Storage{mem1, mem2})
		require.NoError(t, err)

		value, err := storage.Get(context.Background(), "KEY")
		require.NoError(t, err)
		assert.Equal(t, "value1", value)
	})

	t.Run("fallback to second storage", func(t *testing.T) {
		mem1 := memory.NewStorage(nil)
		mem2 := memory.NewStorage(map[string]string{"KEY": "value2"})

		storage, err := NewStorage([]env.Storage{mem1, mem2})
		require.NoError(t, err)

		value, err := storage.Get(context.Background(), "KEY")
		require.NoError(t, err)
		assert.Equal(t, "value2", value)
	})

	t.Run("not found returns error", func(t *testing.T) {
		mem := memory.NewStorage(nil)

		storage, err := NewStorage([]env.Storage{mem})
		require.NoError(t, err)

		_, err = storage.Get(context.Background(), "NONEXISTENT")
		assert.ErrorIs(t, err, env.ErrVariableNotFound)
	})

	t.Run("empty string is valid value", func(t *testing.T) {
		mem1 := memory.NewStorage(map[string]string{"EMPTY_KEY": ""})
		mem2 := memory.NewStorage(map[string]string{"EMPTY_KEY": "fallback"})

		storage, err := NewStorage([]env.Storage{mem1, mem2})
		require.NoError(t, err)

		// Should return empty string from first storage, not fallback
		value, err := storage.Get(context.Background(), "EMPTY_KEY")
		require.NoError(t, err)
		assert.Equal(t, "", value)
	})

	t.Run("propagates non-NotFound errors", func(t *testing.T) {
		// First storage returns an error other than NotFound
		errStorage := &errorStorage{err: env.ErrStorageReadOnly}
		mem := memory.NewStorage(map[string]string{"KEY": "value"})

		storage, err := NewStorage([]env.Storage{errStorage, mem})
		require.NoError(t, err)

		// Should propagate the error, not fallback
		_, err = storage.Get(context.Background(), "KEY")
		assert.ErrorIs(t, err, env.ErrStorageReadOnly)
	})
}

// errorStorage is a test helper that always returns a specific error
type errorStorage struct {
	err error
}

func (s *errorStorage) Get(_ context.Context, _ string) (string, error) {
	return "", s.err
}

func (s *errorStorage) Set(_ context.Context, _, _ string) error {
	return s.err
}

func (s *errorStorage) Delete(_ context.Context, _ string) error {
	return s.err
}

func (s *errorStorage) List(_ context.Context) (map[string]string, error) {
	return nil, s.err
}

func TestStorage_GetWithFallback(t *testing.T) {
	memStorage := memory.NewStorage(nil)
	osStorage := envos.NewStorage()

	err := memStorage.Set(context.Background(), "TEST_VAR", "memory_value")
	require.NoError(t, err)

	storage, err := NewStorage([]env.Storage{memStorage, osStorage})
	require.NoError(t, err)

	value, err := storage.Get(context.Background(), "TEST_VAR")
	require.NoError(t, err)
	assert.Equal(t, "memory_value", value)

	_, err = storage.Get(context.Background(), "NONEXISTENT_VAR")
	assert.ErrorIs(t, err, env.ErrVariableNotFound)
}

func TestStorage_SetToPrimaryOnly(t *testing.T) {
	memStorage := memory.NewStorage(nil)
	osStorage := envos.NewStorage()

	storage, err := NewStorage([]env.Storage{memStorage, osStorage})
	require.NoError(t, err)

	err = storage.Set(context.Background(), "ROUTER_VAR", "router_value")
	require.NoError(t, err)

	value, err := memStorage.Get(context.Background(), "ROUTER_VAR")
	require.NoError(t, err)
	assert.Equal(t, "router_value", value)

	_, err = osStorage.Get(context.Background(), "ROUTER_VAR")
	assert.ErrorIs(t, err, env.ErrVariableNotFound)
}

func TestStorage_Delete(t *testing.T) {
	memStorage := memory.NewStorage(map[string]string{"KEY1": "value1"})

	storage, err := NewStorage([]env.Storage{memStorage})
	require.NoError(t, err)

	err = storage.Delete(context.Background(), "KEY1")
	require.NoError(t, err)

	_, err = memStorage.Get(context.Background(), "KEY1")
	assert.ErrorIs(t, err, env.ErrVariableNotFound)
}

func TestStorage_ListCombinesAllStorages(t *testing.T) {
	mem1 := memory.NewStorage(map[string]string{"MEM1_VAR": "mem1_value"})
	mem2 := memory.NewStorage(map[string]string{"MEM2_VAR": "mem2_value"})

	storage, err := NewStorage([]env.Storage{mem1, mem2})
	require.NoError(t, err)

	values, err := storage.List(context.Background())
	require.NoError(t, err)

	assert.Contains(t, values, "MEM1_VAR")
	assert.Contains(t, values, "MEM2_VAR")
	assert.Equal(t, "mem1_value", values["MEM1_VAR"])
	assert.Equal(t, "mem2_value", values["MEM2_VAR"])
}

func TestStorage_ListPriorityOrder(t *testing.T) {
	mem1 := memory.NewStorage(map[string]string{"SHARED_VAR": "first_value"})
	mem2 := memory.NewStorage(map[string]string{"SHARED_VAR": "second_value"})

	storage, err := NewStorage([]env.Storage{mem1, mem2})
	require.NoError(t, err)

	values, err := storage.List(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "first_value", values["SHARED_VAR"])
}

func TestStorage_CachesValues(t *testing.T) {
	memStorage := memory.NewStorage(map[string]string{"CACHED_VAR": "cached_value"})

	storage, err := NewStorage([]env.Storage{memStorage})
	require.NoError(t, err)

	value1, err := storage.Get(context.Background(), "CACHED_VAR")
	require.NoError(t, err)
	assert.Equal(t, "cached_value", value1)

	err = memStorage.Set(context.Background(), "CACHED_VAR", "updated_value")
	require.NoError(t, err)

	value2, err := storage.Get(context.Background(), "CACHED_VAR")
	require.NoError(t, err)
	assert.Equal(t, "cached_value", value2)
}

func TestStorage_Concurrent(t *testing.T) {
	mem := memory.NewStorage(map[string]string{"KEY": "value"})
	storage, err := NewStorage([]env.Storage{mem})
	require.NoError(t, err)

	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(3)

		go func() {
			defer wg.Done()
			_, _ = storage.Get(ctx, "KEY")
		}()

		go func() {
			defer wg.Done()
			_ = storage.Set(ctx, "KEY", "new_value")
		}()

		go func() {
			defer wg.Done()
			_, _ = storage.List(ctx)
		}()
	}

	wg.Wait()
}

// Benchmarks

func BenchmarkStorage_Get(b *testing.B) {
	mem := memory.NewStorage(map[string]string{"KEY": "value"})
	storage, _ := NewStorage([]env.Storage{mem})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.Get(ctx, "KEY")
	}
}

func BenchmarkStorage_GetCached(b *testing.B) {
	mem := memory.NewStorage(map[string]string{"KEY": "value"})
	storage, _ := NewStorage([]env.Storage{mem})
	ctx := context.Background()

	_, _ = storage.Get(ctx, "KEY")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.Get(ctx, "KEY")
	}
}

func BenchmarkStorage_GetFallback(b *testing.B) {
	mem1 := memory.NewStorage(nil)
	mem2 := memory.NewStorage(map[string]string{"KEY": "value"})
	storage, _ := NewStorage([]env.Storage{mem1, mem2})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.Get(ctx, "KEY")
	}
}

func BenchmarkStorage_Set(b *testing.B) {
	mem := memory.NewStorage(nil)
	storage, _ := NewStorage([]env.Storage{mem})
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
	mem := memory.NewStorage(data)
	storage, _ := NewStorage([]env.Storage{mem})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.List(ctx)
	}
}

func BenchmarkStorage_GetParallel(b *testing.B) {
	mem := memory.NewStorage(map[string]string{"KEY": "value"})
	storage, _ := NewStorage([]env.Storage{mem})
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = storage.Get(ctx, "KEY")
		}
	})
}
