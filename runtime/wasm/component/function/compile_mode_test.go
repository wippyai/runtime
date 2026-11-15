// SPDX-License-Identifier: MPL-2.0
package function

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero/api"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

func TestSharedRuntimePreservesAsyncHostImports(t *testing.T) {
	ctx := context.Background()
	rt, err := wasmrt.New(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close(ctx) })
	// A sibling entry has already registered this asynchronous host. The next
	// entry may resolve it even when its configured import list is empty.
	require.NoError(t, rt.RegisterCoreFunc("env", "pause", nil, []api.ValueType{api.ValueTypeI32}, func(_ context.Context, _ api.Module, stack []uint64) { stack[0] = 42 }, true))
	mod, err := rt.LoadWAT(ctx, `(module (import "env" "pause" (func $pause (result i32))) (memory (export "memory") 1) (func (export "run") (result i32) call $pause))`, "run: func() -> u32;")
	require.NoError(t, err)
	m := NewManager(zap.NewNop(), nil, nil, nil, wasmcomponent.InMemoryCaches())
	require.NoError(t, m.compileModule(ctx, mod, nil, false))
	// Asyncify controls are only present after the transform. This must not
	// depend on the next entry repeating the sibling's configured import.
	instance, err := mod.Instantiate(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = instance.Close(ctx) })
	require.NotNil(t, instance.GetExportedFunction("asyncify_start_unwind"), "shared runtime skipped instrumentation for an actual async import")
}
