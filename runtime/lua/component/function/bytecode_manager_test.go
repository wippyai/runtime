package function

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	systempayload "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap"
)

type mockFSRegistry struct{}

func (m *mockFSRegistry) Register(name string, fs fs.FS) error {
	return nil
}

func (m *mockFSRegistry) Get(name string) (fs.FS, bool) {
	return nil, false
}

func (m *mockFSRegistry) Unregister(name string) bool {
	return false
}

func TestNewBytecodeManager(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	disp := &mockDispatcher{}
	fsReg := &mockFSRegistry{}

	manager := NewBytecodeManager(log, codeManager, bus, disp, fsReg)

	assert.NotNil(t, manager)
	assert.Equal(t, log, manager.log)
	assert.Equal(t, codeManager, manager.code)
	assert.Equal(t, bus, manager.bus)
	assert.Equal(t, disp, manager.dispatcher)
	assert.Equal(t, fsReg, manager.fsRegistry)
	assert.NotNil(t, manager.pools)
}

func TestBytecodeManager_Add_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	disp := &mockDispatcher{}
	fsReg := &mockFSRegistry{}
	manager := NewBytecodeManager(log, codeManager, bus, disp, fsReg)

	entry := registry.Entry{
		Kind: registry.Kind("invalid"),
	}

	err := manager.Add(context.Background(), entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestBytecodeManager_Add_InvalidConfig(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	disp := &mockDispatcher{}
	fsReg := &mockFSRegistry{}
	manager := NewBytecodeManager(log, codeManager, bus, disp, fsReg)

	testData := `{"path": "test", "invalid": }`

	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: api.KindFunctionBytecode,
		Data: payloadData,
	}

	ctx := context.Background()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	err := manager.Add(ctx, entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unpack function config")
}

func TestBytecodeManager_Update_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	disp := &mockDispatcher{}
	fsReg := &mockFSRegistry{}
	manager := NewBytecodeManager(log, codeManager, bus, disp, fsReg)

	entry := registry.Entry{
		Kind: registry.Kind("invalid"),
	}

	err := manager.Update(context.Background(), entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestBytecodeManager_Delete_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	disp := &mockDispatcher{}
	fsReg := &mockFSRegistry{}
	manager := NewBytecodeManager(log, codeManager, bus, disp, fsReg)

	entry := registry.Entry{
		Kind: registry.Kind("invalid"),
	}

	err := manager.Delete(context.Background(), entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestBytecodeManager_Invalidate(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	disp := &mockDispatcher{}
	fsReg := &mockFSRegistry{}
	manager := NewBytecodeManager(log, codeManager, bus, disp, fsReg)

	ids := []registry.ID{{Name: "test1"}, {Name: "test2"}}
	manager.Invalidate(context.Background(), ids)
}

func TestBytecodeManager_StartStop(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	disp := &mockDispatcher{}
	fsReg := &mockFSRegistry{}
	manager := NewBytecodeManager(log, codeManager, bus, disp, fsReg)

	ctx := context.Background()
	err := manager.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, manager.started)

	manager.Stop()
	assert.False(t, manager.started)
	assert.Empty(t, manager.pools)
}
