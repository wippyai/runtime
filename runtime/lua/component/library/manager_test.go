package library

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/code"
	systempayload "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewManager(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}

	manager := NewManager(log, codeManager)

	assert.NotNil(t, manager)
	assert.Equal(t, log, manager.log)
	assert.Equal(t, codeManager, manager.code)
}

func TestManager_Add_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	manager := NewManager(log, codeManager)

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
	manager := NewManager(log, codeManager)

	// Create invalid JSON
	testData := `{"source": "test", "invalid": }`

	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: api.KindLibrary,
		Data: payloadData,
	}

	// Create context with transcoder
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	err := manager.Add(ctx, entry)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unpack library config")
}

func TestManager_Update_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	manager := NewManager(log, codeManager)

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
	manager := NewManager(log, codeManager)

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
	manager := NewManager(log, codeManager)

	// Test with some IDs
	ids := []registry.ID{{Name: "test1"}, {Name: "test2"}}
	manager.Invalidate(context.Background(), ids)

	// Should not panic and just log
}
