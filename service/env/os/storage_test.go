package os

import (
	"context"
	stdos "os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/env"
	enverr "github.com/wippyai/runtime/service/env"
)

func TestStorage_ImplementsInterface(_ *testing.T) {
	var _ env.Storage = (*Storage)(nil)
}

func TestStorage_Get(t *testing.T) {
	storage := NewStorage()

	t.Run("existing env var", func(t *testing.T) {
		err := stdos.Setenv("TEST_ENV_VAR", "test_value")
		require.NoError(t, err)
		defer func() { _ = stdos.Unsetenv("TEST_ENV_VAR") }()

		value, err := storage.Get(context.Background(), "TEST_ENV_VAR")
		require.NoError(t, err)
		assert.Equal(t, "test_value", value)
	})

	t.Run("non-existent env var", func(t *testing.T) {
		_, err := storage.Get(context.Background(), "NONEXISTENT_TEST_VAR_12345")
		assert.ErrorIs(t, err, env.ErrVariableNotFound)
	})

	t.Run("empty env var is valid value", func(t *testing.T) {
		err := stdos.Setenv("TEST_EMPTY_VAR", "")
		require.NoError(t, err)
		defer func() { _ = stdos.Unsetenv("TEST_EMPTY_VAR") }()

		value, err := storage.Get(context.Background(), "TEST_EMPTY_VAR")
		require.NoError(t, err)
		assert.Equal(t, "", value)
	})
}

func TestStorage_Set(t *testing.T) {
	storage := NewStorage()

	err := storage.Set(context.Background(), "KEY", "VALUE")
	assert.ErrorIs(t, err, enverr.ErrStorageReadOnly)
}

func TestStorage_Delete(t *testing.T) {
	storage := NewStorage()

	err := storage.Delete(context.Background(), "KEY")
	assert.ErrorIs(t, err, enverr.ErrStorageReadOnly)
}

func TestStorage_List(t *testing.T) {
	storage := NewStorage()

	err := stdos.Setenv("TEST_LIST_VAR", "list_value")
	require.NoError(t, err)
	defer func() { _ = stdos.Unsetenv("TEST_LIST_VAR") }()

	values, err := storage.List(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, values)
	assert.Contains(t, values, "TEST_LIST_VAR")
	assert.Equal(t, "list_value", values["TEST_LIST_VAR"])
}

func TestStaticStorage_ImplementsInterface(_ *testing.T) {
	var _ env.Storage = (*StaticStorage)(nil)
}

func TestStaticStorage_Get(t *testing.T) {
	data := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	}
	storage := NewStaticStorage(data)

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

func TestStaticStorage_Set(t *testing.T) {
	storage := NewStaticStorage(nil)

	err := storage.Set(context.Background(), "KEY", "VALUE")
	assert.ErrorIs(t, err, enverr.ErrStorageReadOnly)
}

func TestStaticStorage_Delete(t *testing.T) {
	storage := NewStaticStorage(nil)

	err := storage.Delete(context.Background(), "KEY")
	assert.ErrorIs(t, err, enverr.ErrStorageReadOnly)
}

func TestStaticStorage_List(t *testing.T) {
	data := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	}
	storage := NewStaticStorage(data)

	values, err := storage.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, values, 2)
	assert.Equal(t, "value1", values["KEY1"])
	assert.Equal(t, "value2", values["KEY2"])
}

func TestStaticStorage_IsolatesData(t *testing.T) {
	data := map[string]string{
		"KEY1": "value1",
	}
	storage := NewStaticStorage(data)

	data["KEY1"] = "modified"
	data["KEY2"] = "new"

	value, err := storage.Get(context.Background(), "KEY1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)

	_, err = storage.Get(context.Background(), "KEY2")
	assert.ErrorIs(t, err, env.ErrVariableNotFound)
}

func TestStaticStorage_NilData(t *testing.T) {
	storage := NewStaticStorage(nil)

	values, err := storage.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, values)
}

func TestStaticStorage_Concurrent(_ *testing.T) {
	data := map[string]string{"KEY": "value"}
	storage := NewStaticStorage(data)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)

		go func() {
			defer wg.Done()
			_, _ = storage.Get(ctx, "KEY")
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
	_ = stdos.Setenv("BENCH_KEY", "value")
	defer func() { _ = stdos.Unsetenv("BENCH_KEY") }()

	storage := NewStorage()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.Get(ctx, "BENCH_KEY")
	}
}

func BenchmarkStorage_List(b *testing.B) {
	storage := NewStorage()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.List(ctx)
	}
}

func BenchmarkStaticStorage_Get(b *testing.B) {
	storage := NewStaticStorage(map[string]string{"KEY": "value"})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.Get(ctx, "KEY")
	}
}

func BenchmarkStaticStorage_List(b *testing.B) {
	data := make(map[string]string)
	for i := 0; i < 100; i++ {
		data["KEY"+string(rune('A'+i))] = "value"
	}
	storage := NewStaticStorage(data)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.List(ctx)
	}
}

func BenchmarkStaticStorage_GetParallel(b *testing.B) {
	storage := NewStaticStorage(map[string]string{"KEY": "value"})
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = storage.Get(ctx, "KEY")
		}
	})
}
