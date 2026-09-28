// SPDX-License-Identifier: MPL-2.0
package sockets

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestDNSNameMemoryPreflight(t *testing.T) {
	ctx := t.Context()
	runtime := wazero.NewRuntime(ctx)
	t.Cleanup(func() { require.NoError(t, runtime.Close(ctx)) })
	wasm, err := wat.Compile(`(module (memory (export "memory") 1))`)
	require.NoError(t, err)
	module, err := runtime.Instantiate(ctx, wasm)
	require.NoError(t, err)
	require.NoError(t, validateResolveNameMemory(ctx, module, []uint64{1, 64, maxResolveNameBytes, 4096}))
	require.ErrorContains(t, validateResolveNameMemory(ctx, module, []uint64{1, 64, maxResolveNameBytes + 1, 4096}), "host byte limit")
	require.ErrorContains(t, validateResolveNameMemory(ctx, module, []uint64{1, 65535, 2, 4096}), "outside guest memory")
	require.Error(t, validateResolveNameMemory(ctx, module, []uint64{1, 64, 1}))
	require.ErrorContains(t, validateResolveNameMemory(ctx, nil, []uint64{1, 64, 1, 4096}), "unavailable")
}
