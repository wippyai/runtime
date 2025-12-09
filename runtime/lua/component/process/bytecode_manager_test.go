package process

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

func (m *mockFSRegistry) GetFS(_ string) (fs.FS, bool) {
	return nil, false
}

func TestNewBytecodeManager(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	fsReg := &mockFSRegistry{}

	manager := NewBytecodeManager(log, codeManager, bus, fsReg)

	assert.NotNil(t, manager)
	assert.Equal(t, log, manager.log)
	assert.Equal(t, codeManager, manager.code)
	assert.Equal(t, bus, manager.bus)
	assert.Equal(t, fsReg, manager.fsRegistry)
}

func TestBytecodeManager_Add_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	fsReg := &mockFSRegistry{}
	manager := NewBytecodeManager(log, codeManager, bus, fsReg)

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
	fsReg := &mockFSRegistry{}
	manager := NewBytecodeManager(log, codeManager, bus, fsReg)

	testData := `{"path": "test", "invalid": }`

	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: api.KindProcessBytecode,
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

func TestBytecodeManager_Update_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	fsReg := &mockFSRegistry{}
	manager := NewBytecodeManager(log, codeManager, bus, fsReg)

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
	fsReg := &mockFSRegistry{}
	manager := NewBytecodeManager(log, codeManager, bus, fsReg)

	entry := registry.Entry{
		Kind: registry.Kind("invalid"),
	}

	err := manager.Delete(context.Background(), entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestBytecodeManager_Invalidate(_ *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := &mockEventBus{}
	fsReg := &mockFSRegistry{}
	manager := NewBytecodeManager(log, codeManager, bus, fsReg)

	ids := []registry.ID{{Name: "test1"}, {Name: "test2"}}
	manager.Invalidate(context.Background(), ids)
}
