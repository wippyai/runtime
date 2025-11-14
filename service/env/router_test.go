package env

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/env"
	"go.uber.org/zap"
)

func TestRouterStorage_GetWithFallback(t *testing.T) {
	logger := zap.NewNop()

	// Create test storages
	memoryStorage := NewMemoryStorage(nil, logger)
	osStorage := NewOSStorage(logger)

	// Set a value in memory storage
	err := memoryStorage.Set(context.Background(), "TEST_VAR", "memory_value")
	assert.NoError(t, err)

	// Create router storage with memory first (primary), then OS
	routerStorage, err := NewRouterStorage([]env.Storage{memoryStorage, osStorage}, logger)
	assert.NoError(t, err)

	// Test getting value from primary storage
	value, err := routerStorage.Get(context.Background(), "TEST_VAR")
	assert.NoError(t, err)
	assert.Equal(t, "memory_value", value)

	// Test getting a value that doesn't exist in any storage
	_, err = routerStorage.Get(context.Background(), "NONEXISTENT_VAR")
	assert.Error(t, err)
}

func TestRouterStorage_Simple(t *testing.T) {
	logger := zap.NewNop()

	// Create test storages
	memoryStorage := NewMemoryStorage(nil, logger)
	osStorage := NewOSStorage(logger)

	// Create router storage
	routerStorage, err := NewRouterStorage([]env.Storage{memoryStorage, osStorage}, logger)
	assert.NoError(t, err)

	// Test getting a value that doesn't exist
	value, err := routerStorage.Get(context.Background(), "NONEXISTENT_VAR")
	t.Logf("Result: value='%s', err=%v", value, err)

	// The router should return an error from the last storage (OS storage)
	assert.Error(t, err)
}

func TestRouterStorage_SetToPrimaryOnly(t *testing.T) {
	logger := zap.NewNop()

	// Create test storages
	memoryStorage := NewMemoryStorage(nil, logger)
	osStorage := NewOSStorage(logger)

	// Create router storage
	routerStorage, err := NewRouterStorage([]env.Storage{memoryStorage, osStorage}, logger)
	assert.NoError(t, err)

	// Set a value through router
	err = routerStorage.Set(context.Background(), "ROUTER_VAR", "router_value")
	assert.NoError(t, err)

	// Verify it's in the primary storage (memory)
	value, err := memoryStorage.Get(context.Background(), "ROUTER_VAR")
	assert.NoError(t, err)
	assert.Equal(t, "router_value", value)

	// Verify it's not in the secondary storage (OS)
	_, err = osStorage.Get(context.Background(), "ROUTER_VAR")
	assert.Error(t, err)
}

func TestRouterStorage_ListCombinesAllStorages(t *testing.T) {
	logger := zap.NewNop()

	// Create test storages
	memoryStorage := NewMemoryStorage(nil, logger)
	osStorage := NewOSStorage(logger)

	// Set values in memory storage
	err := memoryStorage.Set(context.Background(), "MEMORY_VAR", "memory_value")
	assert.NoError(t, err)

	// Set values in OS storage (this will fail since OS storage is read-only, but we can test the structure)
	// Note: OS storage is read-only, so we can't actually set values in tests

	// Create router storage
	routerStorage, err := NewRouterStorage([]env.Storage{memoryStorage, osStorage}, logger)
	assert.NoError(t, err)

	// List all variables
	variables, err := routerStorage.List(context.Background())
	assert.NoError(t, err)

	// Should contain the memory variable
	assert.Contains(t, variables, "MEMORY_VAR")
	assert.Equal(t, "memory_value", variables["MEMORY_VAR"])
}

func TestRouterStorage_EmptyStoragesError(t *testing.T) {
	logger := zap.NewNop()

	// Try to create router storage with no storages
	_, err := NewRouterStorage([]env.Storage{}, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one storage must be provided")
}
