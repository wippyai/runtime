// SPDX-License-Identifier: MPL-2.0

package library

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
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

func TestNewManager(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	fsReg := &mockFSRegistry{}

	manager := NewManager(log, codeManager, fsReg)

	assert.NotNil(t, manager)
	assert.Equal(t, log, manager.log)
	assert.Equal(t, codeManager, manager.code)
	assert.Equal(t, fsReg, manager.fsRegistry)
}

func TestManager_Add_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	fsReg := &mockFSRegistry{}
	manager := NewManager(log, codeManager, fsReg)

	entry := registry.Entry{
		Kind: "invalid",
	}

	err := manager.Add(context.Background(), entry)

	require.ErrorContains(t, err, "invalid entry kind")
}

func TestManager_Add_InvalidConfig(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	fsReg := &mockFSRegistry{}
	manager := NewManager(log, codeManager, fsReg)

	testData := `{"source": "test", "invalid": }`

	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: api.Library,
		Data: payloadData,
	}

	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)

	err := manager.Add(ctx, entry)

	require.ErrorContains(t, err, "failed to unpack library config")
}

func TestManager_Update_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	fsReg := &mockFSRegistry{}
	manager := NewManager(log, codeManager, fsReg)

	entry := registry.Entry{
		Kind: "invalid",
	}

	err := manager.Update(context.Background(), entry)

	require.ErrorContains(t, err, "invalid entry kind")
}

func TestManager_Delete_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	fsReg := &mockFSRegistry{}
	manager := NewManager(log, codeManager, fsReg)

	entry := registry.Entry{
		Kind: "invalid",
	}

	err := manager.Delete(context.Background(), entry)

	require.ErrorContains(t, err, "invalid entry kind")
}

func TestManager_Invalidate(_ *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	fsReg := &mockFSRegistry{}
	manager := NewManager(log, codeManager, fsReg)

	ids := []registry.ID{{Name: "test1"}, {Name: "test2"}}
	manager.Invalidate(context.Background(), ids)
}

func TestManager_Add_BytecodeKind_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	fsReg := &mockFSRegistry{}
	manager := NewManager(log, codeManager, fsReg)

	entry := registry.Entry{
		Kind: "invalid",
	}

	err := manager.Add(context.Background(), entry)

	require.ErrorContains(t, err, "invalid entry kind")
}
