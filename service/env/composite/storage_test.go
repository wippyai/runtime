package composite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/service/env/memory"
	envos "github.com/wippyai/runtime/service/env/os"
	"go.uber.org/zap"
)

func TestNewStorage(t *testing.T) {
	logger := zap.NewNop()

	t.Run("with storages", func(t *testing.T) {
		memStorage := memory.NewStorage(nil, logger)
		storage, err := NewStorage([]env.Storage{memStorage}, logger)
		require.NoError(t, err)
		assert.NotNil(t, storage)
	})

	t.Run("empty storages error", func(t *testing.T) {
		_, err := NewStorage([]env.Storage{}, logger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one storage must be provided")
	})
}

func TestStorage_GetWithFallback(t *testing.T) {
	logger := zap.NewNop()

	memStorage := memory.NewStorage(nil, logger)
	osStorage := envos.NewStorage(logger)

	err := memStorage.Set(context.Background(), "TEST_VAR", "memory_value")
	require.NoError(t, err)

	storage, err := NewStorage([]env.Storage{memStorage, osStorage}, logger)
	require.NoError(t, err)

	value, err := storage.Get(context.Background(), "TEST_VAR")
	require.NoError(t, err)
	assert.Equal(t, "memory_value", value)

	_, err = storage.Get(context.Background(), "NONEXISTENT_VAR")
	assert.Error(t, err)
}

func TestStorage_SetToPrimaryOnly(t *testing.T) {
	logger := zap.NewNop()

	memStorage := memory.NewStorage(nil, logger)
	osStorage := envos.NewStorage(logger)

	storage, err := NewStorage([]env.Storage{memStorage, osStorage}, logger)
	require.NoError(t, err)

	err = storage.Set(context.Background(), "ROUTER_VAR", "router_value")
	require.NoError(t, err)

	value, err := memStorage.Get(context.Background(), "ROUTER_VAR")
	require.NoError(t, err)
	assert.Equal(t, "router_value", value)

	_, err = osStorage.Get(context.Background(), "ROUTER_VAR")
	assert.Error(t, err)
}

func TestStorage_Delete(t *testing.T) {
	logger := zap.NewNop()

	memStorage := memory.NewStorage(map[string]string{"KEY1": "value1"}, logger)

	storage, err := NewStorage([]env.Storage{memStorage}, logger)
	require.NoError(t, err)

	err = storage.Delete(context.Background(), "KEY1")
	require.NoError(t, err)

	_, err = memStorage.Get(context.Background(), "KEY1")
	assert.Error(t, err)
}

func TestStorage_ListCombinesAllStorages(t *testing.T) {
	logger := zap.NewNop()

	mem1 := memory.NewStorage(map[string]string{"MEM1_VAR": "mem1_value"}, logger)
	mem2 := memory.NewStorage(map[string]string{"MEM2_VAR": "mem2_value"}, logger)

	storage, err := NewStorage([]env.Storage{mem1, mem2}, logger)
	require.NoError(t, err)

	values, err := storage.List(context.Background())
	require.NoError(t, err)

	assert.Contains(t, values, "MEM1_VAR")
	assert.Contains(t, values, "MEM2_VAR")
	assert.Equal(t, "mem1_value", values["MEM1_VAR"])
	assert.Equal(t, "mem2_value", values["MEM2_VAR"])
}

func TestStorage_ListPriorityOrder(t *testing.T) {
	logger := zap.NewNop()

	mem1 := memory.NewStorage(map[string]string{"SHARED_VAR": "first_value"}, logger)
	mem2 := memory.NewStorage(map[string]string{"SHARED_VAR": "second_value"}, logger)

	storage, err := NewStorage([]env.Storage{mem1, mem2}, logger)
	require.NoError(t, err)

	values, err := storage.List(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "first_value", values["SHARED_VAR"])
}

func TestStorage_CachesValues(t *testing.T) {
	logger := zap.NewNop()

	memStorage := memory.NewStorage(map[string]string{"CACHED_VAR": "cached_value"}, logger)

	storage, err := NewStorage([]env.Storage{memStorage}, logger)
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
