package wat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/runtime/wasm/host"
	systempayload "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
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

type mockDispatcher struct{}

func (m *mockDispatcher) Dispatch(_ dispatcher.Command) dispatcher.Handler {
	return nil
}

func setupTestContext() context.Context {
	ctx := context.Background()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	return payload.WithTranscoder(ctx, transcoder)
}

func TestNewManager(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	hosts := host.NewRegistry()

	manager := NewManager(log, bus, disp, nil, hosts)

	assert.NotNil(t, manager)
	assert.Equal(t, log, manager.log)
	assert.Equal(t, bus, manager.bus)
	assert.Equal(t, disp, manager.dispatcher)
	assert.Equal(t, hosts, manager.hosts)
	assert.NotNil(t, manager.pools)
	assert.NotNil(t, manager.configs)
}

func TestManager_StartStop(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	hosts := host.NewRegistry()
	manager := NewManager(log, bus, disp, nil, hosts)

	ctx := context.Background()
	err := manager.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, manager.started)

	manager.Stop()
	assert.False(t, manager.started)
	assert.Empty(t, manager.pools)
}

func TestManager_Add_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	hosts := host.NewRegistry()
	manager := NewManager(log, bus, disp, nil, hosts)

	entry := registry.Entry{
		Kind: "invalid",
	}

	err := manager.Add(context.Background(), entry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Add_InvalidConfig(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	hosts := host.NewRegistry()
	manager := NewManager(log, bus, disp, nil, hosts)

	testData := `{"source": "test", "invalid": }`
	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: wasmapi.FunctionWAT,
		Data: payloadData,
	}

	ctx := setupTestContext()
	err := manager.Add(ctx, entry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unpack")
}

func TestManager_Update_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	hosts := host.NewRegistry()
	manager := NewManager(log, bus, disp, nil, hosts)

	entry := registry.Entry{
		Kind: "invalid",
	}

	err := manager.Update(context.Background(), entry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Delete_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	hosts := host.NewRegistry()
	manager := NewManager(log, bus, disp, nil, hosts)

	entry := registry.Entry{
		Kind: "invalid",
	}

	err := manager.Delete(context.Background(), entry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Delete_ValidKind(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	hosts := host.NewRegistry()
	manager := NewManager(log, bus, disp, nil, hosts)

	entry := registry.Entry{
		ID:   registry.NewID("test", "func"),
		Kind: wasmapi.FunctionWAT,
	}

	err := manager.Delete(context.Background(), entry)

	assert.NoError(t, err)
}

func TestManager_Execute_PoolNotFound(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	hosts := host.NewRegistry()
	manager := NewManager(log, bus, disp, nil, hosts)

	task := runtime.Task{
		ID: registry.NewID("test", "nonexistent"),
	}

	result, err := manager.Execute(context.Background(), task)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "pool not found")
}

func TestManager_ConfigOperations(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	hosts := host.NewRegistry()
	manager := NewManager(log, bus, disp, nil, hosts)

	id := registry.NewID("test", "config")
	cfg := &configEntry{
		method: "main",
		pool:   wasmapi.PoolConfig{Size: 4},
	}

	manager.storeConfig(id, cfg)

	manager.mu.RLock()
	retrieved := manager.configs[id]
	manager.mu.RUnlock()

	require.NotNil(t, retrieved)
	assert.Equal(t, "main", retrieved.method)
	assert.Equal(t, 4, retrieved.pool.Size)

	manager.deleteConfig(id)

	manager.mu.RLock()
	deleted := manager.configs[id]
	manager.mu.RUnlock()
	assert.Nil(t, deleted)
}

func TestManager_RemovePool_NonExistent(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	hosts := host.NewRegistry()
	manager := NewManager(log, bus, disp, nil, hosts)

	// Should not panic
	manager.removePool(registry.NewID("test", "nonexistent"))
}

func TestManager_PoolTypes(t *testing.T) {
	tests := []struct {
		name     string
		poolType string
	}{
		{"inline", wasmapi.PoolTypeInline},
		{"lazy", wasmapi.PoolTypeLazy},
		{"static", wasmapi.PoolTypeStatic},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := wasmapi.PoolConfig{
				Type: tt.poolType,
				Size: 4,
			}
			assert.Equal(t, tt.poolType, cfg.Type)
		})
	}
}

func TestManager_Stop_WithActivePools(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	hosts := host.NewRegistry()
	manager := NewManager(log, bus, disp, nil, hosts)

	err := manager.Start(context.Background())
	require.NoError(t, err)

	manager.Stop()
	assert.Empty(t, manager.pools)
	assert.False(t, manager.started)
}

func TestManager_CreateExecutionHooks_NoTopology(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	hosts := host.NewRegistry()
	manager := NewManager(log, bus, disp, nil, hosts)

	hooks := manager.createExecutionHooks()

	assert.Nil(t, hooks.OnStart)
	assert.Nil(t, hooks.OnComplete)
}

func TestManager_Concurrency(t *testing.T) {
	log := zap.NewNop()
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	hosts := host.NewRegistry()
	manager := NewManager(log, bus, disp, nil, hosts)

	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			id := registry.NewID("test", "concurrent")
			cfg := &configEntry{method: "test"}
			manager.storeConfig(id, cfg)
			manager.deleteConfig(id)
		}
		done <- struct{}{}
	}()

	go func() {
		for i := 0; i < 100; i++ {
			manager.removePool(registry.NewID("test", "pool"))
		}
		done <- struct{}{}
	}()

	<-done
	<-done
}

func TestConfigEntry_Fields(t *testing.T) {
	cfg := &configEntry{
		method: "main",
		pool: wasmapi.PoolConfig{
			Type:   wasmapi.PoolTypeStatic,
			Size:   8,
			Buffer: 256,
		},
		config: &wasmapi.WATFunctionConfig{
			Source: "(module)",
			Method: "main",
		},
	}

	assert.Equal(t, "main", cfg.method)
	assert.Equal(t, wasmapi.PoolTypeStatic, cfg.pool.Type)
	assert.Equal(t, 8, cfg.pool.Size)
	assert.NotNil(t, cfg.config)
}

func TestPoolEntry_Fields(t *testing.T) {
	entry := &poolEntry{
		pool:   nil,
		method: "run",
	}

	assert.Nil(t, entry.pool)
	assert.Equal(t, "run", entry.method)
}
