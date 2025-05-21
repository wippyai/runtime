package env

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewMemoryStorage(t *testing.T) {
	defaultValues := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}
	logger := zap.NewNop()
	storage := NewMemoryStorage(defaultValues, logger)

	// Verify default values were set
	value1, err := storage.Get(context.Background(), "key1")
	assert.NoError(t, err)
	assert.Equal(t, "value1", value1)

	value2, err := storage.Get(context.Background(), "key2")
	assert.NoError(t, err)
	assert.Equal(t, "value2", value2)
}

func TestMemoryStorage_Get(t *testing.T) {
	storage := NewMemoryStorage(nil, zap.NewNop())

	// Test getting non-existent key
	value, err := storage.Get(context.Background(), "nonexistent")
	assert.NoError(t, err)
	assert.Empty(t, value)

	// Test getting existing key
	err = storage.Set(context.Background(), "test", "value")
	assert.NoError(t, err)

	value, err = storage.Get(context.Background(), "test")
	assert.NoError(t, err)
	assert.Equal(t, "value", value)
}

func TestMemoryStorage_Set(t *testing.T) {
	storage := NewMemoryStorage(nil, zap.NewNop())

	// Test setting a new value
	err := storage.Set(context.Background(), "key", "value")
	assert.NoError(t, err)

	value, err := storage.Get(context.Background(), "key")
	assert.NoError(t, err)
	assert.Equal(t, "value", value)

	// Test overwriting existing value
	err = storage.Set(context.Background(), "key", "newvalue")
	assert.NoError(t, err)

	value, err = storage.Get(context.Background(), "key")
	assert.NoError(t, err)
	assert.Equal(t, "newvalue", value)
}

func TestMemoryStorage_Delete(t *testing.T) {
	storage := NewMemoryStorage(nil, zap.NewNop())

	// Set up test data
	err := storage.Set(context.Background(), "key", "value")
	assert.NoError(t, err)

	// Test deleting existing key
	err = storage.Delete(context.Background(), "key")
	assert.NoError(t, err)

	value, err := storage.Get(context.Background(), "key")
	assert.NoError(t, err)
	assert.Empty(t, value)

	// Test deleting non-existent key
	err = storage.Delete(context.Background(), "nonexistent")
	assert.NoError(t, err)
}

func TestMemoryStorage_List(t *testing.T) {
	storage := NewMemoryStorage(nil, zap.NewNop())

	// Set up test data
	testData := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	for k, v := range testData {
		err := storage.Set(context.Background(), k, v)
		assert.NoError(t, err)
	}

	// Test listing all values
	values, err := storage.List(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, testData, values)
}

func TestMemoryStorage_ServiceLifecycle(t *testing.T) {
	storage := NewMemoryStorage(nil, zap.NewNop())

	// Test Start
	statusCh, err := storage.Start(context.Background())
	assert.NoError(t, err)
	status := <-statusCh
	assert.Equal(t, supervisor.Running, status)

	// Test Stop
	err = storage.Stop(context.Background())
	assert.NoError(t, err)
}

func TestMemoryStorage_ResourceManagement(t *testing.T) {
	storage := NewMemoryStorage(nil, zap.NewNop())
	id := registry.ID{NS: "test", Name: "id"}

	// Test acquiring resource
	res, err := storage.Acquire(context.Background(), id, resource.ModeNormal)
	assert.NoError(t, err)
	assert.NotNil(t, res)

	// Test getting resource
	storageValue, err := res.Get()
	assert.NoError(t, err)
	assert.Equal(t, storage, storageValue)

	// Test releasing resource
	res.Release()

	// Test operations after release
	_, err = res.Get()
	assert.Error(t, err)
	assert.Equal(t, resource.ErrResourceClosed, err)

	// Test acquiring with invalid mode
	_, err = storage.Acquire(context.Background(), id, resource.ModeExclusive)
	assert.Error(t, err)
	assert.Equal(t, resource.ErrResourceLocked, err)
}
