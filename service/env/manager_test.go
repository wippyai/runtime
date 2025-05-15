package env

import (
	"context"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	serviceenv "github.com/ponyruntime/pony/api/service/env"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockTranscoder implements payload.Transcoder for testing
type mockTranscoder struct{}

func (m *mockTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	return payload.NewPayload(p, format), nil
}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	// Type switches based on expected output type and source payload
	switch dest := v.(type) {
	case *serviceenv.CreateMemoryEnvStorageConfig:
		if src, ok := p.Data().(*serviceenv.CreateMemoryEnvStorageConfig); ok {
			*dest = *src
			return nil
		}
	case *serviceenv.CreateFileEnvStorageConfig:
		if src, ok := p.Data().(*serviceenv.CreateFileEnvStorageConfig); ok {
			*dest = *src
			return nil
		}
	case *env.Variable:
		if src, ok := p.Data().(*env.Variable); ok {
			*dest = *src
			return nil
		}
	}

	return nil
}

//nolint:unparam // ok for now
func setupTestManager(_ *testing.T) (*Manager, *eventbus.Bus) {
	bus := eventbus.NewBus()
	logger := zap.NewNop()
	dtt := &mockTranscoder{}
	manager := NewManager(bus, dtt, logger)

	return manager, bus
}

func TestNewManager(t *testing.T) {
	bus := eventbus.NewBus()
	logger := zap.NewNop()
	dtt := &mockTranscoder{}

	manager := NewManager(bus, dtt, logger)

	assert.NotNil(t, manager)
	assert.Equal(t, bus, manager.bus)
	assert.Equal(t, dtt, manager.dtt)
	assert.Equal(t, logger, manager.logger)
	assert.NotNil(t, manager.storages)
	assert.NotNil(t, manager.factory)
}

func TestManager_Add_MemoryStorage(t *testing.T) {
	manager, _ := setupTestManager(t)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "memory"},
		Kind: env.KindStorageMemory,
		Data: payload.New(&serviceenv.CreateMemoryEnvStorageConfig{
			Lifecycle: supervisor.LifecycleConfig{
				StartTimeout: time.Second,
				StopTimeout:  time.Second,
			},
		}),
	}

	err := manager.Add(context.Background(), entry)
	require.NoError(t, err)

	// Verify storage was added
	manager.mu.RLock()
	storage, exists := manager.storages[entry.ID]
	manager.mu.RUnlock()

	assert.True(t, exists)
	assert.NotNil(t, storage)
}

func TestManager_Acquire(t *testing.T) {
	manager, _ := setupTestManager(t)

	// Add a memory storage first
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "memory"},
		Kind: env.KindStorageMemory,
		Data: payload.New(&serviceenv.CreateMemoryEnvStorageConfig{
			Lifecycle: supervisor.LifecycleConfig{
				StartTimeout: time.Second,
				StopTimeout:  time.Second,
			},
		}),
	}

	err := manager.Add(context.Background(), entry)
	require.NoError(t, err)

	// Test successful acquisition
	res, err := manager.Acquire(context.Background(), entry.ID, resource.ModeNormal)
	require.NoError(t, err)
	assert.NotNil(t, res)

	// Test acquisition with non-existent storage
	_, err = manager.Acquire(context.Background(), registry.ID{NS: "test", Name: "non-existent"}, resource.ModeNormal)
	assert.Error(t, err)

	// Test acquisition with unsupported mode
	_, err = manager.Acquire(context.Background(), entry.ID, resource.ModeExclusive)
	assert.Equal(t, resource.ErrResourceLocked, err)
}
