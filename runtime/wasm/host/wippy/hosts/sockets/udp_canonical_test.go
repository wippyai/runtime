// SPDX-License-Identifier: MPL-2.0
package sockets

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestUDPDatagramPreflightBounds(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { require.NoError(t, rt.Close(ctx)) })
	code, err := wat.Compile(`(module (memory (export "memory") 2))`)
	require.NoError(t, err)
	mod, err := rt.Instantiate(ctx, code)
	require.NoError(t, err)
	header := make([]byte, outgoingDatagramStride)
	binary.LittleEndian.PutUint32(header, 1024)
	binary.LittleEndian.PutUint32(header[4:], maxUDPDatagramBytes)
	require.True(t, mod.Memory().Write(64, header))
	require.NoError(t, validateUDPDatagrams(ctx, mod, []uint64{1, 64, 1, 512}))
	require.NoError(t, validateUDPDatagrams(ctx, mod, []uint64{1, 0, 0, 512}))
	require.Error(t, validateUDPDatagrams(ctx, mod, []uint64{1, 64, 1}))
	require.ErrorContains(t, validateUDPDatagrams(ctx, mod, []uint64{1, 64, ^uint64(0), 512}), "batch limit")
	require.ErrorContains(t, validateUDPDatagrams(ctx, mod, []uint64{1, 131060, 1, 512}), "list outside")
	require.ErrorContains(t, validateUDPDatagrams(ctx, nil, []uint64{1, 64, 1, 512}), "memory unavailable")
	// Ordinary oversized packets reach the semantic datagram-too-large check.
	binary.LittleEndian.PutUint32(header[4:], maxUDPDatagramBytes+1)
	require.True(t, mod.Memory().Write(64, header))
	require.NoError(t, validateUDPDatagrams(ctx, mod, []uint64{1, 64, 1, 512}))
	binary.LittleEndian.PutUint32(header[4:], maxUDPBatchBytes+1)
	require.True(t, mod.Memory().Write(64, header))
	require.ErrorContains(t, validateUDPDatagrams(ctx, mod, []uint64{1, 64, 1, 512}), "host byte budget")
	binary.LittleEndian.PutUint32(header, 131070)
	binary.LittleEndian.PutUint32(header[4:], 4)
	require.True(t, mod.Memory().Write(64, header))
	require.ErrorContains(t, validateUDPDatagrams(ctx, mod, []uint64{1, 64, 1, 512}), "data outside")
	// A malformed later element must be checked, not just the first header.
	binary.LittleEndian.PutUint32(header, 1024)
	binary.LittleEndian.PutUint32(header[4:], 1)
	require.True(t, mod.Memory().Write(64, header))
	binary.LittleEndian.PutUint32(header[4:], maxUDPBatchBytes+1)
	require.True(t, mod.Memory().Write(64+outgoingDatagramStride, header))
	require.ErrorContains(t, validateUDPDatagrams(ctx, mod, []uint64{1, 64, 2, 512}), "host byte budget")
}
