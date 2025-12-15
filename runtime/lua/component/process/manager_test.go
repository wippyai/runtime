package process

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/fs"
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

type mockFSRegistry struct{}

func (m *mockFSRegistry) GetFS(_ string) (fs.FS, bool) {
	return nil, false
}

type mockCompiledFactory struct{}

func (m *mockCompiledFactory) CreateFactory(_ registry.ID, _ ...engine.FactoryOption) (processapi.FactoryFunc, error) {
	return func() (processapi.Process, error) {
		return nil, nil
	}, nil
}

func TestNewManager(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	fsReg := &mockFSRegistry{}
	factory := &mockCompiledFactory{}

	manager := NewManager(log, codeManager, bus, fsReg, factory)

	assert.NotNil(t, manager)
	assert.Equal(t, log, manager.log)
	assert.Equal(t, codeManager, manager.code)
	assert.Equal(t, bus, manager.bus)
	assert.Equal(t, fsReg, manager.fsRegistry)
	assert.Equal(t, factory, manager.factory)
}

func TestManager_Add_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	fsReg := &mockFSRegistry{}
	factory := &mockCompiledFactory{}
	manager := NewManager(log, codeManager, bus, fsReg, factory)

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
	fsReg := &mockFSRegistry{}
	factory := &mockCompiledFactory{}
	manager := NewManager(log, codeManager, bus, fsReg, factory)

	testData := `{"source": "test", "invalid": }`

	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: api.Process,
		Data: payloadData,
	}

	ctx := context.Background()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	err := manager.Add(ctx, entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unpack process config")
}

func TestManager_Update_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	fsReg := &mockFSRegistry{}
	factory := &mockCompiledFactory{}
	manager := NewManager(log, codeManager, bus, fsReg, factory)

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
	fsReg := &mockFSRegistry{}
	factory := &mockCompiledFactory{}
	manager := NewManager(log, codeManager, bus, fsReg, factory)

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
	bus := &mockEventBus{}
	fsReg := &mockFSRegistry{}
	factory := &mockCompiledFactory{}
	manager := NewManager(log, codeManager, bus, fsReg, factory)

	ids := []registry.ID{{Name: "test1"}, {Name: "test2"}}
	manager.Invalidate(context.Background(), ids)
}
