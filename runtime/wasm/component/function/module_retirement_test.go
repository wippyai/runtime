// SPDX-License-Identifier: MPL-2.0

package function

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

var retirementWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7f,
	0x03, 0x02, 0x01, 0x00,
	0x07, 0x07, 0x01, 0x03, 'r', 'u', 'n', 0x00, 0x00,
	0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x07, 0x0b,
}

func TestSharedModuleRetirement(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	m := NewManager(zap.NewNop(), nil, noopDispatcher{}, nil, wasmcomponent.InMemoryCaches())
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)
	load := func() *wasmrt.Module {
		t.Helper()
		module, err := m.coreRT.LoadWASM(ctx, retirementWASM, "")
		require.NoError(t, err)
		require.NoError(t, module.Compile(ctx))
		return module
	}
	id := registry.NewID("test", "module")
	cfg := &configEntry{kind: wasmapi.FunctionWASM, method: "run", pool: wasmapi.PoolConfig{Type: "lazy"}}
	old := load()
	require.NoError(t, m.createPool(id, cfg, old))
	for range 5 {
		next := load()
		require.NoError(t, m.replacePool(id, cfg, next))
		require.ErrorContains(t, old.Compile(ctx), "closed", "obsolete generation is still retained")
		require.NoError(t, next.Compile(ctx), "retiring a generation closed its sibling")
		old = next
	}
	m.removePool(id)
	require.ErrorContains(t, old.Compile(ctx), "closed")
	require.NoError(t, load().Compile(ctx), "retirement shut down the shared runtime")
}

func TestModuleRetirementWaitsForActiveLease(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	m := NewManager(zap.NewNop(), nil, noopDispatcher{}, nil, wasmcomponent.InMemoryCaches())
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)
	module, err := m.coreRT.LoadWASM(ctx, retirementWASM, "")
	require.NoError(t, err)
	id := registry.NewID("test", "active")
	cfg := &configEntry{kind: wasmapi.FunctionWASM, method: "run", pool: wasmapi.PoolConfig{Type: "lazy"}}
	require.NoError(t, m.createPool(id, cfg, module))
	entry := m.pools[id]
	require.True(t, entry.acquire())
	m.removePool(id)
	require.NoError(t, module.Compile(ctx), "active generation was closed early")
	entry.release()
	require.Eventually(t, func() bool { return module.Compile(ctx) != nil }, time.Second, time.Millisecond)
}

func TestFailedPoolCreationReleasesModule(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	m := NewManager(zap.NewNop(), nil, noopDispatcher{}, nil, wasmcomponent.InMemoryCaches())
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)
	module, err := m.coreRT.LoadWASM(ctx, retirementWASM, "")
	require.NoError(t, err)
	cfg := &configEntry{kind: wasmapi.FunctionWASM, method: "run", pool: wasmapi.PoolConfig{Type: "invalid"}}
	require.Error(t, m.createPool(registry.NewID("test", "invalid"), cfg, module))
	require.ErrorContains(t, module.Compile(ctx), "closed", "failed generation was retained")
}

func TestFailedReplacementPreservesCallableGeneration(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	m := NewManager(zap.NewNop(), nil, poolTestDispatcher{}, nil, wasmcomponent.InMemoryCaches())
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)
	load := func() *wasmrt.Module {
		t.Helper()
		mod, err := m.coreRT.LoadWAT(ctx, `(module (func (export "run") (result i32) i32.const 42))`, "run: func() -> s32;")
		require.NoError(t, err)
		require.NoError(t, mod.Compile(ctx))
		return mod
	}
	id := registry.NewID("test", "replacement")
	old, rejected := load(), load()
	cfg := &configEntry{kind: wasmapi.FunctionWAT, method: "run", pool: wasmapi.PoolConfig{Type: "lazy"}}
	require.NoError(t, m.createPool(id, cfg, old))
	bad := *cfg
	bad.pool.Type = "invalid"
	require.Error(t, m.replacePool(id, &bad, rejected))
	require.ErrorContains(t, rejected.Compile(ctx), "closed")
	require.NoError(t, old.Compile(ctx))
	result, err := m.Execute(ctx, runtimeapi.Task{ID: id})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NoError(t, result.Error)
	require.Equal(t, "42", fmt.Sprint(result.Value.Data()))
	m.removePool(id)
	m.removePool(id)
	require.ErrorContains(t, old.Compile(ctx), "closed")
	m.Stop()
	m.Stop()
}

func TestIsolatedComponentPoolReleasesUnusedPreflightModule(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	m := NewManager(zap.NewNop(), nil, noopDispatcher{}, nil, wasmcomponent.InMemoryCaches())
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)
	module, err := m.coreRT.LoadWASM(ctx, retirementWASM, "")
	require.NoError(t, err)
	// The lazy factory does not instantiate here; the preflight module is never
	// used by the isolated-module factory, regardless of guest contents.
	cfg := &configEntry{component: true, kind: wasmapi.FunctionWASM, method: "run", pool: wasmapi.PoolConfig{Type: "lazy"}}
	require.NoError(t, m.createPool(registry.NewID("test", "isolated"), cfg, module))
	require.ErrorContains(t, module.Compile(ctx), "closed", "unused validation copy is still retained")
}
