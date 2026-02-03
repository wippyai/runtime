package workflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	systempayload "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap"
)

const invalid registry.Kind = "invalid.kind"

type mockEventBus struct {
	events []event.Event
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

func (m *mockEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {
}

type mockCompiledFactory struct{}

func (m *mockCompiledFactory) CreateFactory(_ registry.ID, _ ...engine.FactoryOption) (processapi.FactoryFunc, error) {
	return func() (processapi.Process, error) {
		return nil, fmt.Errorf("mock factory not implemented")
	}, nil
}

func TestNewManager(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	factory := &mockCompiledFactory{}

	manager := NewManager(log, codeManager, bus, factory)

	assert.NotNil(t, manager)
	assert.Equal(t, log, manager.log)
	assert.Equal(t, codeManager, manager.code)
	assert.Equal(t, bus, manager.bus)
	assert.Equal(t, factory, manager.factory)
}

func TestManager_Add_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	factory := &mockCompiledFactory{}
	manager := NewManager(log, codeManager, bus, factory)

	entry := registry.Entry{
		Kind: invalid,
	}

	err := manager.Add(context.Background(), entry)

	require.ErrorContains(t, err, "invalid entry kind")
}

func TestManager_Add_InvalidConfig(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	factory := &mockCompiledFactory{}
	manager := NewManager(log, codeManager, bus, factory)

	testData := `{"source": "test", "invalid": }`

	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: api.Workflow,
		Data: payloadData,
	}

	ctx := context.Background()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	err := manager.Add(ctx, entry)

	require.ErrorContains(t, err, "failed to unpack workflow config")
}

func TestManager_Update_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	factory := &mockCompiledFactory{}
	manager := NewManager(log, codeManager, bus, factory)

	entry := registry.Entry{
		Kind: invalid,
	}

	err := manager.Update(context.Background(), entry)

	require.ErrorContains(t, err, "invalid entry kind")
}

func TestManager_Delete_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	factory := &mockCompiledFactory{}
	manager := NewManager(log, codeManager, bus, factory)

	entry := registry.Entry{
		Kind: invalid,
	}

	err := manager.Delete(context.Background(), entry)

	require.ErrorContains(t, err, "invalid entry kind")
}

func TestManager_Invalidate_NoConfig(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	factory := &mockTrackingFactory{}
	manager := NewManager(log, codeManager, bus, factory)

	ids := []registry.ID{{Name: "test1"}, {Name: "test2"}}
	manager.Invalidate(context.Background(), ids)

	// Factory should not be called when no configs exist
	assert.Equal(t, 0, factory.callCount)
}

func TestManager_Invalidate_WithConfig(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	factory := &mockTrackingFactory{}
	manager := NewManager(log, codeManager, bus, factory)

	id := registry.NewID("test", "workflow")
	cfg := &api.WorkflowConfig{
		Source: "return {}",
		Method: "main",
	}
	manager.configs.Store(id, cfg)

	manager.Invalidate(context.Background(), []registry.ID{id})

	// Factory should be called for recompilation
	assert.Equal(t, 1, factory.callCount)
}

func TestManager_Update_InvalidConfig(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	factory := &mockCompiledFactory{}
	manager := NewManager(log, codeManager, bus, factory)

	testData := `{"source": "test", "invalid": }`
	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: api.Workflow,
		Data: payloadData,
	}

	ctx := context.Background()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	err := manager.Update(ctx, entry)

	require.ErrorContains(t, err, "failed to unpack workflow config")
}

func TestManager_ConfigStoreLoad(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	factory := &mockCompiledFactory{}
	manager := NewManager(log, codeManager, bus, factory)

	id := registry.NewID("test", "config")
	cfg := &api.WorkflowConfig{
		Source: "return { main = function() end }",
		Method: "main",
	}

	// Store config
	manager.configs.Store(id, cfg)

	// Load config
	loaded, ok := manager.configs.Load(id)
	require.True(t, ok)

	loadedCfg := loaded.(*api.WorkflowConfig)
	assert.Equal(t, cfg.Source, loadedCfg.Source)
	assert.Equal(t, cfg.Method, loadedCfg.Method)

	// Delete config
	manager.configs.Delete(id)

	// Verify deleted
	_, ok = manager.configs.Load(id)
	assert.False(t, ok)
}

func TestManager_unregisterFactory_SendsEvent(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	factory := &mockCompiledFactory{}
	manager := NewManager(log, codeManager, bus, factory)

	id := registry.NewID("test", "unregister")
	manager.unregisterFactory(context.Background(), id)

	require.Len(t, bus.events, 1)
	assert.Equal(t, processapi.System, bus.events[0].System)
	assert.Equal(t, processapi.FactoryDelete, bus.events[0].Kind)
	assert.Equal(t, id.String(), bus.events[0].Path)
}

type mockTrackingFactory struct {
	callCount int
}

func (m *mockTrackingFactory) CreateFactory(_ registry.ID, _ ...engine.FactoryOption) (processapi.FactoryFunc, error) {
	m.callCount++
	return func() (processapi.Process, error) {
		return nil, fmt.Errorf("mock factory not implemented")
	}, nil
}

func TestManager_Concurrency(_ *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	factory := &mockCompiledFactory{}
	manager := NewManager(log, codeManager, bus, factory)

	done := make(chan struct{})

	// Concurrent config operations
	go func() {
		for i := 0; i < 100; i++ {
			id := registry.NewID("test", "concurrent")
			cfg := &api.WorkflowConfig{Source: "test"}
			manager.configs.Store(id, cfg)
			manager.configs.Load(id)
			manager.configs.Delete(id)
		}
		done <- struct{}{}
	}()

	// Concurrent invalidate
	go func() {
		for i := 0; i < 50; i++ {
			manager.Invalidate(context.Background(), []registry.ID{
				registry.NewID("test", "inv1"),
				registry.NewID("test", "inv2"),
			})
		}
		done <- struct{}{}
	}()

	<-done
	<-done
}
