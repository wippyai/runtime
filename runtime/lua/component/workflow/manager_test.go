package workflow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	systempayload "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap"
)

// simpleEventBus implements event.Bus for testing
type simpleEventBus struct {
	events []event.Event
}

func (s *simpleEventBus) Send(_ context.Context, e event.Event) {
	s.events = append(s.events, e)
}

func (s *simpleEventBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}

func (s *simpleEventBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}

func (s *simpleEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {
}

func TestNewManager(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}

	manager := NewManager(log, codeManager, bus)

	assert.NotNil(t, manager)
	assert.Equal(t, log, manager.log)
	assert.Equal(t, codeManager, manager.code)
	assert.Equal(t, bus, manager.bus)
}

func TestManager_Add_InvalidConfig(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}
	manager := NewManager(log, codeManager, bus)

	// Create invalid JSON
	testData := `{"source": "test", "method": "test", "invalid": }`

	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: api.KindWorkflow,
		Data: payloadData,
	}

	// Create context with transcoder
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	err := manager.Add(ctx, entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unpack workflow config")
}

func TestManager_Add_EmptySource(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}
	manager := NewManager(log, codeManager, bus)

	// Create config with empty source
	cfg := api.ProcessConfig{
		Source: "",
		Method: "test_method",
	}

	// Create context with transcoder
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	// Transcode config to payload
	pp, err := transcoder.Transcode(payload.New(cfg), payload.JSON)
	assert.NoError(t, err)

	entry := registry.Entry{
		ID:   registry.ParseID("app:test_workflow"),
		Kind: api.KindWorkflow,
		Data: pp,
	}

	err = manager.Add(ctx, entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workflow source cannot be empty")
}

func TestManager_Add_EmptyMethod(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}
	manager := NewManager(log, codeManager, bus)

	// Create config with empty method
	cfg := api.ProcessConfig{
		Source: "function test() end",
		Method: "",
	}

	// Create context with transcoder
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	// Transcode config to payload
	pp, err := transcoder.Transcode(payload.New(cfg), payload.JSON)
	assert.NoError(t, err)

	entry := registry.Entry{
		ID:   registry.ParseID("app:test_workflow"),
		Kind: api.KindWorkflow,
		Data: pp,
	}

	err = manager.Add(ctx, entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workflow method cannot be empty")
}

func TestManager_Update_InvalidConfig(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}
	manager := NewManager(log, codeManager, bus)

	// Create invalid JSON
	testData := `{"source": "test", "method": "test", "invalid": }`

	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: api.KindWorkflow,
		Data: payloadData,
	}

	// Create context with transcoder
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	err := manager.Update(ctx, entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unpack workflow config")
}

func TestManager_Invalidate(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}
	manager := NewManager(log, codeManager, bus)

	// Store some configs first
	id1 := registry.ParseID("app:workflow1")
	id2 := registry.ParseID("app:workflow2")

	cfg := api.ProcessConfig{
		Source: "function test() return 'test' end",
		Method: "test",
	}

	manager.configs.Store(id1, cfg)
	manager.configs.Store(id2, cfg)

	// Invalidate
	ids := []registry.ID{id1, id2}
	manager.Invalidate(context.Background(), ids)

	// Should not panic
	// Configs should still exist (not deleted by invalidate)
	_, exists := manager.configs.Load(id1)
	assert.True(t, exists)
	_, exists = manager.configs.Load(id2)
	assert.True(t, exists)
}

func TestManager_Invalidate_NonExistentID(_ *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}
	manager := NewManager(log, codeManager, bus)

	// Invalidate non-existent IDs
	ids := []registry.ID{
		registry.ParseID("app:nonexistent1"),
		registry.ParseID("app:nonexistent2"),
	}
	manager.Invalidate(context.Background(), ids)

	// Should not panic
}

func TestManager_PrototypeRegistration(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}
	manager := NewManager(log, codeManager, bus)

	ctx := ctxapi.NewRootContext()
	id := registry.ParseID("app:test_workflow")

	// Manually store config
	cfg := api.ProcessConfig{
		Source: "function test() return 'test' end",
		Method: "test",
	}
	manager.configs.Store(id, cfg)

	// Test registerPrototype
	manager.registerPrototype(ctx, id)

	// Verify event was sent to bus for registration
	assert.Len(t, bus.events, 1)
	assert.Equal(t, "prototype", string(bus.events[0].System))
	assert.Equal(t, "prototype.register", string(bus.events[0].Kind))
	assert.Equal(t, id.String(), bus.events[0].Path)

	// Test unregisterPrototype
	bus.events = nil
	manager.unregisterPrototype(ctx, id)

	// Verify delete event was sent
	assert.Len(t, bus.events, 1)
	assert.Equal(t, "prototype", string(bus.events[0].System))
	assert.Equal(t, "prototype.delete", string(bus.events[0].Kind))
	assert.Equal(t, id.String(), bus.events[0].Path)
}

func TestManager_Update_ReregistersPrototype(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}
	manager := NewManager(log, codeManager, bus)

	ctx := ctxapi.NewRootContext()
	id := registry.ParseID("app:test_workflow")

	// Manually store config
	cfg := api.ProcessConfig{
		Source: "function test() return 'v1' end",
		Method: "test",
	}
	manager.configs.Store(id, cfg)

	// Call upsertPrototype to register
	err := manager.upsertPrototype(ctx, id)
	assert.NoError(t, err)

	// Verify registration event was sent
	assert.Len(t, bus.events, 1)
	assert.Equal(t, "prototype", string(bus.events[0].System))
	assert.Equal(t, "prototype.register", string(bus.events[0].Kind))

	// Update config
	bus.events = nil
	cfg.Source = "function test() return 'v2' end"
	manager.configs.Store(id, cfg)

	// Call upsertPrototype again (simulating Update)
	err = manager.upsertPrototype(ctx, id)
	assert.NoError(t, err)

	// Verify registration event was sent again
	assert.Len(t, bus.events, 1)
	assert.Equal(t, "prototype", string(bus.events[0].System))
	assert.Equal(t, "prototype.register", string(bus.events[0].Kind))
}
