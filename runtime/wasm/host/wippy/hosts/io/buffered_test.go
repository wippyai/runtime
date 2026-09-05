// SPDX-License-Identifier: MPL-2.0
package io

import (
	"bytes"
	"context"
	stdio "io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
	"github.com/wippyai/runtime/api/dispatcher"
	socketapi "github.com/wippyai/runtime/api/socket"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
	"github.com/wippyai/wasm-runtime/wat"
)

func bufferedHostContext(t *testing.T) (context.Context, *wasmengine.Scheduler, *wasmengine.Asyncify, *wippyhost.AsyncValueStore) {
	t.Helper()
	async := wasmengine.NewAsyncify()
	scheduler := wasmengine.NewScheduler(async)
	store := wippyhost.NewAsyncValueStore()
	ctx := wippyhost.WithAsyncValueStore(wasmengine.WithScheduler(wasmengine.WithAsyncify(context.Background(), async), scheduler), store)
	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { require.NoError(t, rt.Close(ctx)) })
	code, err := wat.Compile(`(module (func (export "noop")))`)
	require.NoError(t, err)
	mod, err := rt.Instantiate(ctx, code)
	require.NoError(t, err)
	require.NoError(t, scheduler.Execute(ctx, mod.ExportedFunction("noop")))
	return ctx, scheduler, async, store
}

func bufferedPair(t *testing.T) (*StreamsHost, *preview2.ResourceTable, uint32, uint32, net.Conn) {
	t.Helper()
	table := preview2.NewResourceTableWithLimits(16, 1)
	left, right := net.Pipe()
	socket := preview2.NewTCPSocketResource(4)
	socket.SetConn(left)
	socket.SetState(preview2.TCPStateConnected)
	table.Add(socket)
	input := table.Add(preview2.NewTCPInputStreamResource(socket))
	output := table.Add(preview2.NewTCPOutputStreamResource(socket))
	t.Cleanup(func() { _ = right.Close(); require.NoError(t, table.Close()) })
	return NewStreamsHost(table), table, input, output, right
}

func TestBufferedInputReadSuspendsAndResumes(t *testing.T) {
	host, table, input, _, peer := bufferedPair(t)
	ctx, scheduler, async, store := bufferedHostContext(t)
	data, streamErr := host.MethodInputStreamRead(ctx, input, 32)
	require.Nil(t, streamErr)
	require.Empty(t, data)
	subscription := host.MethodInputStreamSubscribe(ctx, input)
	resource, ok := table.Get(subscription)
	require.True(t, ok)
	require.False(t, resource.(preview2.Pollable).Ready())
	data, streamErr = host.MethodInputStreamBlockingRead(ctx, input, 32)
	require.Nil(t, streamErr)
	require.Empty(t, data)
	require.True(t, async.IsUnwinding(ctx))
	step, err := scheduler.Step(ctx, nil)
	require.NoError(t, err)
	command := step.PendingOp.(interface{ ToCommand() dispatcher.Command }).ToCommand().(*socketapi.PollWaitCmd)
	go func() { _, _ = peer.Write([]byte("hello")) }()
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	indexes, err := command.Wait(waitCtx)
	require.NoError(t, err)
	_, err = scheduler.Step(ctx, &wasmengine.YieldResult{Value: store.Put(indexes)})
	require.Error(t, err) // Host-only fixture: noop has no rewind import.
	data, streamErr = host.MethodInputStreamBlockingRead(ctx, input, 32)
	require.Nil(t, streamErr)
	require.Equal(t, "hello", string(data))
	require.True(t, async.IsNormal(ctx))
	require.False(t, resource.(preview2.Pollable).Ready())
	host.ResourceDropInputStream(ctx, input)
	require.True(t, resource.(preview2.Pollable).Ready())
}

func TestBufferedOutputFlushResumesWithoutRepeatingWrite(t *testing.T) {
	host, _, _, output, peer := bufferedPair(t)
	ctx, scheduler, async, store := bufferedHostContext(t)
	contents := bytes.Repeat([]byte("x"), 4096)
	returned := make(chan *preview2.StreamError, 1)
	go func() { returned <- host.MethodOutputStreamBlockingWriteAndFlush(ctx, output, contents) }()
	select {
	case err := <-returned:
		require.Nil(t, err)
	case <-time.After(time.Second):
		t.Fatal("host blocked on network output")
	}
	require.True(t, async.IsUnwinding(ctx))
	step, err := scheduler.Step(ctx, nil)
	require.NoError(t, err)
	command := step.PendingOp.(interface{ ToCommand() dispatcher.Command }).ToCommand().(*socketapi.StreamWaitCmd)
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	completed := make(chan any, 1)
	go func() { completed <- command.Run(waitCtx) }()
	select {
	case <-completed:
		t.Fatal("flush completed while peer had not read")
	case <-time.After(10 * time.Millisecond):
	}
	received := make([]byte, len(contents))
	_, err = stdio.ReadFull(peer, received)
	require.NoError(t, err)
	require.Equal(t, contents, received)
	var result any
	select {
	case result = <-completed:
	case <-waitCtx.Done():
		t.Fatal("flush did not finish")
	}
	require.Nil(t, result.(*outputCompletion).err)
	_, err = scheduler.Step(ctx, &wasmengine.YieldResult{Value: store.Put(result)})
	require.Error(t, err)
	require.Nil(t, host.MethodOutputStreamBlockingWriteAndFlush(ctx, output, contents))
	require.True(t, async.IsNormal(ctx), "rewind must not write and suspend again")
}

func TestBufferedStreamSubscriptionDropAndPermit(t *testing.T) {
	host, table, input, output, peer := bufferedPair(t)
	ctx := context.Background()
	subscription := host.MethodInputStreamSubscribe(ctx, input)
	table.Remove(subscription)
	source, ok := table.Get(input)
	require.True(t, ok)
	go func() { _, _ = peer.Write([]byte("live")) }()
	signal := source.(interface{ Notify() <-chan struct{} }).Notify()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("subscription drop closed parent")
	}
	data, err := host.MethodInputStreamRead(ctx, input, 4)
	require.Nil(t, err)
	require.Equal(t, "live", string(data))
	require.Panics(t, func() { host.MethodOutputStreamWrite(ctx, output, []byte("no permit")) })
	require.Panics(t, func() { host.MethodOutputStreamBlockingWriteAndFlush(ctx, output, make([]byte, 4097)) })
	require.Error(t, validateBlockingOutputWrite(ctx, nil, []uint64{uint64(output), 0, 4097, 0}))
}

func TestStreamFailuresReturnOwnedErrorHandles(t *testing.T) {
	host, table, input, _, _ := bufferedPair(t)
	_, err := host.MethodInputStreamRead(context.Background(), input, preview2.MaxAllocationSize+1)
	require.NotNil(t, err)
	require.True(t, err.LastOpFailed)
	require.NotZero(t, err.LastOpFailedErr)
	resource, ok := table.Get(err.LastOpFailedErr)
	require.True(t, ok)
	require.Equal(t, preview2.ResourceError, resource.Type())
	table.Remove(err.LastOpFailedErr)
	_, ok = table.Get(err.LastOpFailedErr)
	require.False(t, ok)
}
