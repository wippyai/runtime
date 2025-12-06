package interceptor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/runtime"
	"go.uber.org/zap"
)

func setupRegistryTest() *Registry {
	logger := zap.NewNop()
	reg := NewInterceptorRegistry(logger)
	return reg
}

func TestRegistry_Register(t *testing.T) {
	reg := setupRegistryTest()

	interceptor := &mockInterceptor{name: "test"}
	err := reg.Register("test", interceptor, 100)

	require.NoError(t, err)

	reg.mu.Lock()
	entriesCount := len(reg.entries)
	reg.mu.Unlock()

	assert.Equal(t, 1, entriesCount)
}

func TestRegistry_RegisterWithOrder(t *testing.T) {
	reg := setupRegistryTest()

	interceptor := &mockInterceptor{name: "test"}
	err := reg.Register("test", interceptor, 50)

	require.NoError(t, err)

	reg.mu.Lock()
	entriesCount := len(reg.entries)
	firstOrder := 0
	if entriesCount > 0 {
		firstOrder = reg.entries[0].order
	}
	reg.mu.Unlock()

	assert.Equal(t, 1, entriesCount)
	assert.Equal(t, 50, firstOrder)
}

func TestRegistry_RegisterDuplicate(t *testing.T) {
	reg := setupRegistryTest()

	interceptor := &mockInterceptor{name: "test"}
	err := reg.Register("test", interceptor, 100)
	require.NoError(t, err)

	err = reg.Register("test", interceptor, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")

	reg.mu.Lock()
	entriesCount := len(reg.entries)
	reg.mu.Unlock()

	assert.Equal(t, 1, entriesCount)
}

func TestRegistry_Unregister(t *testing.T) {
	reg := setupRegistryTest()

	interceptor := &mockInterceptor{name: "test"}
	err := reg.Register("test", interceptor, 100)
	require.NoError(t, err)

	err = reg.Unregister("test")
	require.NoError(t, err)

	reg.mu.Lock()
	entriesCount := len(reg.entries)
	reg.mu.Unlock()

	assert.Equal(t, 0, entriesCount)
}

func TestRegistry_UnregisterNotFound(t *testing.T) {
	reg := setupRegistryTest()

	err := reg.Unregister("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRegistry_OrderPreservation(t *testing.T) {
	reg := setupRegistryTest()

	int1 := &mockInterceptor{name: "int1"}
	int2 := &mockInterceptor{name: "int2"}
	int3 := &mockInterceptor{name: "int3"}

	require.NoError(t, reg.Register("int2", int2, 200))
	require.NoError(t, reg.Register("int1", int1, 100))
	require.NoError(t, reg.Register("int3", int3, 300))

	reg.mu.Lock()
	entries := make([]entry, len(reg.entries))
	copy(entries, reg.entries)
	reg.mu.Unlock()

	require.Len(t, entries, 3)
	assert.Equal(t, 100, entries[0].order)
	assert.Equal(t, 200, entries[1].order)
	assert.Equal(t, 300, entries[2].order)
}

func TestRegistry_Execute(t *testing.T) {
	reg := setupRegistryTest()

	interceptor := &mockInterceptor{name: "test"}
	require.NoError(t, reg.Register("test", interceptor, 100))

	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	task := runtime.Task{}
	result, err := reg.Execute(context.Background(), mockFunc, task)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, interceptor.called.Load())
}
