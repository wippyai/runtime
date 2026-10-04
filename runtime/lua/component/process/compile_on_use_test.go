// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	stdjson "encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"github.com/wippyai/runtime/runtime/lua/engine"
	systempayload "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap"
)

func newCompileCountingManager(t *testing.T) (*Manager, *code.Manager, *mockEventBus, context.Context) {
	t.Helper()
	cm, err := code.NewCodeManager(zap.NewNop(), nil, code.Config{
		Cache: cache.Config{
			Dir:               t.TempDir(),
			Enabled:           true,
			CompileEnabled:    true,
			ToolchainIdentity: "process-compile-on-use-test",
		},
	})
	require.NoError(t, err)
	bus := &mockEventBus{}
	manager := NewManager(zap.NewNop(), cm, bus, &mockFSRegistry{}, engine.NewProcessFactory(cm))

	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx := payload.WithTranscoder(ctxapi.NewRootContext(), transcoder)
	ctx = event.WithAwaitService(ctx, &mockPrepareAwaitService{result: event.AwaitResult{Accepted: true}})
	return manager, cm, bus, ctx
}

func processEntry(t *testing.T, id registry.ID, source string) registry.Entry {
	t.Helper()
	data, err := stdjson.Marshal(map[string]any{"source": source, "method": "main"})
	require.NoError(t, err)
	return registry.Entry{ID: id, Kind: api.Process, Data: payload.NewPayload(string(data), payload.JSON)}
}

func compileReads(cm *code.Manager) uint64 {
	stats := cm.CacheStats()
	return stats.CompileHits + stats.CompileMisses
}

func lastRegisteredFactory(t *testing.T, bus *mockEventBus) processapi.FactoryFunc {
	t.Helper()
	require.NotEmpty(t, bus.events)
	registered, ok := bus.events[len(bus.events)-1].Data.(*processapi.FactoryEntry)
	require.True(t, ok)
	return registered.Factory
}

func TestManager_AddedProcessCompilesOnFirstSpawn(t *testing.T) {
	manager, cm, bus, ctx := newCompileCountingManager(t)
	id := registry.NewID("app.test", "worker")

	require.NoError(t, manager.Add(ctx, processEntry(t, id, `local function main() return 1 end return {main = main}`)))
	assert.Zero(t, compileReads(cm), "registering a process must not compile it")

	proc, err := lastRegisteredFactory(t, bus)()
	require.NoError(t, err)
	proc.Close()
	assert.Equal(t, uint64(1), compileReads(cm))
}

func TestManager_ProcessCompileErrorSurfacesAtSpawn(t *testing.T) {
	manager, _, bus, ctx := newCompileCountingManager(t)
	id := registry.NewID("app.test", "broken")

	require.NoError(t, manager.Add(ctx, processEntry(t, id, `goto nowhere`)))

	_, err := lastRegisteredFactory(t, bus)()
	require.ErrorContains(t, err, "no visible label 'nowhere'")
}
