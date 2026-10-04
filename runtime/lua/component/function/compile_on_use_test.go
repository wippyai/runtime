// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	stdjson "encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"github.com/wippyai/runtime/runtime/lua/engine"
	systempayload "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap"
)

const okFunctionSource = `local function main() return "ok" end return {main = main}`

func newCompileCountingManager(t *testing.T) (*Manager, *code.Manager, context.Context) {
	t.Helper()
	cm, err := code.NewCodeManager(zap.NewNop(), nil, code.Config{
		Cache: cache.Config{
			Dir:               t.TempDir(),
			Enabled:           true,
			CompileEnabled:    true,
			ToolchainIdentity: "function-compile-on-use-test",
		},
	})
	require.NoError(t, err)
	manager := NewManager(zap.NewNop(), cm, newMockEventBus(), &mockDispatcher{}, newMockFSRegistry(), engine.NewProcessFactory(cm))
	t.Cleanup(manager.Stop)

	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx := payload.WithTranscoder(ctxapi.NewRootContext(), transcoder)
	ctx = event.WithAwaitService(ctx, &mockPrepareAwaitService{result: event.AwaitResult{Accepted: true}})
	return manager, cm, ctx
}

func functionEntry(t *testing.T, id registry.ID, source string, pool map[string]any) registry.Entry {
	t.Helper()
	cfg := map[string]any{"source": source, "method": "main"}
	if pool != nil {
		cfg["pool"] = pool
	}
	data, err := stdjson.Marshal(cfg)
	require.NoError(t, err)
	return registry.Entry{ID: id, Kind: api.Function, Data: payload.NewPayload(string(data), payload.JSON)}
}

func compileReads(cm *code.Manager) uint64 {
	stats := cm.CacheStats()
	return stats.CompileHits + stats.CompileMisses
}

func TestManager_OnDemandPoolCompilesOnFirstCall(t *testing.T) {
	manager, cm, ctx := newCompileCountingManager(t)
	id := registry.NewID("app.test", "on_demand")

	require.NoError(t, manager.Add(ctx, functionEntry(t, id, okFunctionSource, nil)))
	assert.Zero(t, compileReads(cm), "registering an on-demand pool must not compile it")
	require.NoError(t, manager.Start(ctx))

	callCtx, _ := ctxapi.OpenFrameContext(ctx)
	result, err := manager.Execute(callCtx, runtime.Task{ID: id})
	require.NoError(t, err)
	require.NoError(t, result.Error)
	assert.Equal(t, uint64(1), compileReads(cm))
}

func TestManager_PrecreatedWorkersCompileAtAdd(t *testing.T) {
	pools := map[string]map[string]any{
		"static":     {"workers": 2, "size": 2},
		"inline":     {"type": api.PoolTypeInline},
		"adaptive":   {"type": api.PoolTypeAdaptive},
		"warm_start": {"warm_start": true},
	}
	for name, pool := range pools {
		t.Run(name, func(t *testing.T) {
			manager, cm, ctx := newCompileCountingManager(t)
			id := registry.NewID("app.test", name)

			require.NoError(t, manager.Add(ctx, functionEntry(t, id, okFunctionSource, pool)))
			assert.Equal(t, uint64(1), compileReads(cm))
		})
	}
}

func TestManager_CompileErrorSurfacesAtFirstCall(t *testing.T) {
	manager, _, ctx := newCompileCountingManager(t)
	id := registry.NewID("app.test", "broken")

	require.NoError(t, manager.Add(ctx, functionEntry(t, id, `goto nowhere`, nil)))
	require.NoError(t, manager.Start(ctx))

	callCtx, _ := ctxapi.OpenFrameContext(ctx)
	_, err := manager.Execute(callCtx, runtime.Task{ID: id})
	require.ErrorContains(t, err, "no visible label 'nowhere'")
}

func TestManager_PrecreatedWorkersRejectCompileErrorAtAdd(t *testing.T) {
	manager, _, ctx := newCompileCountingManager(t)
	id := registry.NewID("app.test", "broken_static")

	err := manager.Add(ctx, functionEntry(t, id, `goto nowhere`, map[string]any{"workers": 2, "size": 2}))
	require.ErrorContains(t, err, "no visible label 'nowhere'")
}
