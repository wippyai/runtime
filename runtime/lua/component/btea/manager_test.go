package btea

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"testing"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/code"
	systempayload "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// simpleEventBus implements event.Bus for testing
type simpleEventBus struct{}

func (s *simpleEventBus) Send(_ context.Context, _ event.Event) {
	// Do nothing for simple tests
}

func (s *simpleEventBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}

func (s *simpleEventBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}

func (s *simpleEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {
	// Do nothing for simple tests
}

func TestNewBteaManager(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}

	manager := NewBteaManager(log, codeManager, bus)

	assert.NotNil(t, manager)
	assert.Equal(t, log, manager.log)
	assert.Equal(t, codeManager, manager.code)
	assert.Equal(t, bus, manager.bus)
}

func TestManager_Add_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}
	manager := NewBteaManager(log, codeManager, bus)

	entry := registry.Entry{
		Kind: registry.Kind("invalid"),
	}

	err := manager.Add(context.Background(), entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Add_InvalidConfig(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}
	manager := NewBteaManager(log, codeManager, bus)

	// Create invalid JSON
	testData := `{"source": "test", "method": "test", "invalid": }`

	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: api.KindBteaApp,
		Data: payloadData,
	}

	// Create context with transcoder
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	err := manager.Add(ctx, entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unpack btea config")
}

func TestManager_Update_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}
	manager := NewBteaManager(log, codeManager, bus)

	entry := registry.Entry{
		Kind: registry.Kind("invalid"),
	}

	err := manager.Update(context.Background(), entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Delete_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}
	manager := NewBteaManager(log, codeManager, bus)

	entry := registry.Entry{
		Kind: registry.Kind("invalid"),
	}

	err := manager.Delete(context.Background(), entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Invalidate(_ *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &simpleEventBus{}
	manager := NewBteaManager(log, codeManager, bus)

	// Test with some IDs
	ids := []registry.ID{{Name: "test1"}, {Name: "test2"}}
	manager.Invalidate(context.Background(), ids)

	// Should not panic and just log
}
