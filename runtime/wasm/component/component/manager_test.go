package component

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"go.uber.org/zap"
)

type mockEventBus struct {
	events []event.Event
}

func newMockEventBus() *mockEventBus {
	return &mockEventBus{}
}

func (m *mockEventBus) Send(_ context.Context, e event.Event) {
	m.events = append(m.events, e)
}

func (m *mockEventBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {}

func TestNewManager(t *testing.T) {
	log := zap.NewNop()
	m := NewManager(log, nil, nil, nil, nil, nil)

	require.NotNil(t, m)
	assert.NotNil(t, m.pools)
	assert.NotNil(t, m.configs)
}

func TestManager_StartStop(t *testing.T) {
	log := zap.NewNop()
	m := NewManager(log, nil, nil, nil, nil, nil)

	err := m.Start(context.Background())
	require.NoError(t, err)

	m.mu.RLock()
	started := m.started
	m.mu.RUnlock()
	assert.True(t, started)

	m.Stop()

	m.mu.RLock()
	stopped := !m.started
	m.mu.RUnlock()
	assert.True(t, stopped)
}

func TestManager_Add_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	m := NewManager(log, nil, nil, nil, nil, nil)

	entry := registry.Entry{
		Kind: "wrong.kind",
	}

	err := m.Add(context.Background(), entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Add_InvalidConfig(t *testing.T) {
	log := zap.NewNop()
	m := NewManager(log, nil, nil, nil, nil, nil)

	entry := registry.Entry{
		Kind: wasmapi.FunctionComponent,
		Data: payload.NewPayload(`{invalid`, payload.JSON),
	}

	err := m.Add(context.Background(), entry)
	require.Error(t, err)
}

func TestManager_Update_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	m := NewManager(log, nil, nil, nil, nil, nil)

	entry := registry.Entry{
		Kind: "wrong.kind",
	}

	err := m.Update(context.Background(), entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Delete_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	m := NewManager(log, nil, nil, nil, nil, nil)

	entry := registry.Entry{
		Kind: "wrong.kind",
	}

	err := m.Delete(context.Background(), entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Delete_ValidKind(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	m := NewManager(log, bus, nil, nil, nil, nil)

	entry := registry.Entry{
		Kind: wasmapi.FunctionComponent,
		ID:   registry.ParseID("test:function"),
	}

	err := m.Delete(context.Background(), entry)
	assert.NoError(t, err)
}

func TestManager_Execute_PoolNotFound(t *testing.T) {
	log := zap.NewNop()
	m := NewManager(log, nil, nil, nil, nil, nil)

	id := registry.ParseID("nonexistent:function")

	m.mu.RLock()
	entry, exists := m.pools[id]
	m.mu.RUnlock()

	assert.False(t, exists)
	assert.Nil(t, entry)
}

func TestManager_ConfigOperations(t *testing.T) {
	log := zap.NewNop()
	m := NewManager(log, nil, nil, nil, nil, nil)

	id := registry.ParseID("test:function")
	cfg := &configEntry{
		method:    "main",
		transport: "custom",
		pool:      wasmapi.PoolConfig{Type: wasmapi.PoolTypeInline},
	}

	m.storeConfig(id, cfg)

	m.mu.RLock()
	stored, exists := m.configs[id]
	m.mu.RUnlock()

	require.True(t, exists)
	assert.Equal(t, "main", stored.method)
	assert.Equal(t, "custom", stored.transport)

	m.deleteConfig(id)

	m.mu.RLock()
	_, exists = m.configs[id]
	m.mu.RUnlock()

	assert.False(t, exists)
}

func TestManager_RemovePool_NonExistent(t *testing.T) {
	log := zap.NewNop()
	m := NewManager(log, nil, nil, nil, nil, nil)

	m.removePool(registry.ParseID("nonexistent:pool"))
}

func TestManager_CreateExecutionHooks_NoTopology(t *testing.T) {
	log := zap.NewNop()
	m := NewManager(log, nil, nil, nil, nil, nil)

	hooks := m.createExecutionHooks()

	assert.Nil(t, hooks.OnStart)
	assert.Nil(t, hooks.OnComplete)
}

func TestConfigEntry_Fields(t *testing.T) {
	cfg := &configEntry{
		method:    "main",
		transport: "wit",
		pool:      wasmapi.PoolConfig{Type: wasmapi.PoolTypeStatic, Size: 8},
		config: &wasmapi.ComponentFunctionConfig{
			FS:   "test:fs",
			Path: "component.wasm",
			Hash: "sha256:abc123",
		},
	}

	assert.Equal(t, "main", cfg.method)
	assert.Equal(t, "wit", cfg.transport)
	assert.Equal(t, wasmapi.PoolTypeStatic, cfg.pool.Type)
	assert.Equal(t, 8, cfg.pool.Size)
	assert.Equal(t, "test:fs", cfg.config.FS)
	assert.Equal(t, "component.wasm", cfg.config.Path)
	assert.Equal(t, "sha256:abc123", cfg.config.Hash)
}

func TestPoolEntry_Fields(t *testing.T) {
	entry := &poolEntry{
		pool:   nil,
		method: "execute",
	}

	assert.Equal(t, "execute", entry.method)
	assert.Nil(t, entry.pool)
}
