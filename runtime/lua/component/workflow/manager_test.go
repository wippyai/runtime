package workflow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	systempayload "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap"
)

type mockEventBus struct {
	events []event.Event
}

func (m *mockEventBus) Send(ctx context.Context, e event.Event) {
	m.events = append(m.events, e)
}

func (m *mockEventBus) Subscribe(ctx context.Context, filter event.Filter, ch chan event.Event) {
}

func (m *mockEventBus) Unsubscribe(ctx context.Context, ch chan event.Event) {
}

func TestNewManager(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}

	manager := NewManager(log, codeManager, bus)

	assert.NotNil(t, manager)
	assert.Equal(t, log, manager.log)
	assert.Equal(t, codeManager, manager.code)
	assert.Equal(t, bus, manager.bus)
}

func TestManager_Add_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	manager := NewManager(log, codeManager, bus)

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
	bus := &mockEventBus{}
	manager := NewManager(log, codeManager, bus)

	testData := `{"source": "test", "invalid": }`

	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: api.KindWorkflow,
		Data: payloadData,
	}

	ctx := context.Background()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	err := manager.Add(ctx, entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unpack workflow config")
}

func TestManager_Update_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	manager := NewManager(log, codeManager, bus)

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
	bus := &mockEventBus{}
	manager := NewManager(log, codeManager, bus)

	entry := registry.Entry{
		Kind: registry.Kind("invalid"),
	}

	err := manager.Delete(context.Background(), entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Invalidate(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	manager := NewManager(log, codeManager, bus)

	ids := []registry.ID{{Name: "test1"}, {Name: "test2"}}
	manager.Invalidate(context.Background(), ids)
}
