// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	socketapi "github.com/wippyai/runtime/api/socket"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var noopWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x04, 0x01, 0x60, 0x00, 0x00,
	0x03, 0x02, 0x01, 0x00,
	0x07, 0x08, 0x01, 0x04, 'n', 'o', 'o', 'p', 0x00, 0x00,
	0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b,
}

type closeCountingConn struct {
	net.Conn
	closes atomic.Int32
}

func (c *closeCountingConn) Close() error {
	c.closes.Add(1)
	return c.Conn.Close()
}

type closeCountingListener struct {
	net.Listener
	closes atomic.Int32
}

func (l *closeCountingListener) Close() error {
	l.closes.Add(1)
	return l.Listener.Close()
}

func rewindContext(t *testing.T, value any) context.Context {
	t.Helper()

	async := wasmengine.NewAsyncify()
	scheduler := wasmengine.NewScheduler(async)
	store := wippyhost.NewAsyncValueStore()
	ctx := wasmengine.WithAsyncify(context.Background(), async)
	ctx = wasmengine.WithScheduler(ctx, scheduler)
	ctx = wippyhost.WithAsyncValueStore(ctx, store)

	runtime := wazero.NewRuntime(ctx)
	t.Cleanup(func() {
		if err := runtime.Close(context.Background()); err != nil {
			t.Errorf("close test WebAssembly runtime: %v", err)
		}
	})
	module, err := runtime.Instantiate(ctx, noopWasm)
	if err != nil {
		t.Fatalf("instantiate rewind helper: %v", err)
	}
	if err := scheduler.Execute(ctx, module.ExportedFunction("noop")); err != nil {
		t.Fatalf("initialize rewind helper: %v", err)
	}
	token := store.Put(value)
	_, stepErr := scheduler.Step(ctx, &wasmengine.YieldResult{Value: token})
	if stepErr == nil || !async.IsRewinding(ctx) {
		t.Fatalf("prepare rewind: error = %v, rewinding = %v", stepErr, async.IsRewinding(ctx))
	}
	return ctx
}

func requireNetworkError(t *testing.T, err *NetworkError, code NetworkErrorCode) {
	t.Helper()
	if err == nil || err.Code != code {
		t.Fatalf("network error = %#v, want code %d", err, code)
	}
}

func TestS01TCPConnectRejectsWrongAsyncType(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPHost(resources)
	socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	socket.SetState(preview2.TCPStateConnectInProgress)
	handle := resources.Add(socket)

	left, right := net.Pipe()
	carried := &closeCountingConn{Conn: left}
	t.Cleanup(func() { _ = right.Close() })
	ctx := rewindContext(t, &socketapi.AcceptResult{Conn: carried})

	err := host.MethodTCPSocketStartConnect(ctx, handle, 0, IPSocketAddress{})
	requireNetworkError(t, err, NetworkErrorInvalidArgument)
	if carried.closes.Load() != 1 {
		t.Fatalf("unadopted connection close count = %d, want 1", carried.closes.Load())
	}
	if socket.Conn() != nil || socket.State() != preview2.TCPStateConnectInProgress {
		t.Fatalf("socket changed after rejected result: conn = %v, state = %d", socket.Conn(), socket.State())
	}
}

func TestS02TCPListenRejectsWrongAsyncType(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPHost(resources)
	socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	socket.SetState(preview2.TCPStateListenInProgress)
	handle := resources.Add(socket)

	left, right := net.Pipe()
	carried := &closeCountingConn{Conn: left}
	t.Cleanup(func() { _ = right.Close() })
	ctx := rewindContext(t, &socketapi.ConnectResult{Conn: carried})

	err := host.MethodTCPSocketStartListen(ctx, handle)
	requireNetworkError(t, err, NetworkErrorInvalidArgument)
	if carried.closes.Load() != 1 {
		t.Fatalf("unadopted resource close count = %d, want 1", carried.closes.Load())
	}
	if socket.Listener() != nil || socket.State() != preview2.TCPStateListenInProgress {
		t.Fatalf("socket changed after rejected result: listener = %v, state = %d", socket.Listener(), socket.State())
	}
}

func TestTCPAcceptRequiresListeningQueue(t *testing.T) {
	resources := preview2.NewResourceTable()
	defer resources.Close()
	host := NewTCPHost(resources)
	socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	handle := resources.Add(socket)
	accepted, err := host.MethodTCPSocketAccept(context.Background(), handle)
	requireNetworkError(t, err, NetworkErrorInvalidState)
	if accepted != nil {
		t.Fatal("accepted child on unbound socket")
	}
}

func TestS06TCPBindStateTransition(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPHost(resources)
	socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	handle := resources.Add(socket)
	address := *SocketAddressFromHostPort("127.0.0.1", 43123)

	if err := host.MethodTCPSocketStartBind(context.Background(), handle, 0, address); err != nil {
		t.Fatalf("start bind: %v", err)
	}
	if socket.State() != preview2.TCPStateBindInProgress {
		t.Fatalf("state after start bind = %d, want bind-in-progress", socket.State())
	}
	if err := host.MethodTCPSocketFinishBind(context.Background(), handle); err != nil {
		t.Fatalf("finish bind: %v", err)
	}
	if socket.State() != preview2.TCPStateBound {
		t.Fatalf("state after finish bind = %d, want bound", socket.State())
	}
	requireNetworkError(t, host.MethodTCPSocketStartBind(context.Background(), handle, 0, *SocketAddressFromHostPort("127.0.0.2", 1)), NetworkErrorInvalidState)
	local, err := host.MethodTCPSocketLocalAddress(context.Background(), handle)
	if err != nil || local == nil || !local.Equal(&address) {
		t.Fatalf("local address after repeated bind = %#v, error = %v, want %#v", local, err, address)
	}
}

func TestS07TCPFinishConnectFailureClosesState(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPHost(resources)
	socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	socket.SetState(preview2.TCPStateConnectInProgress)
	handle := resources.Add(socket)
	ctx := rewindContext(t, &socketapi.ConnectResult{Err: fmt.Errorf("dial failed: %w", syscall.ECONNREFUSED)})

	if err := host.MethodTCPSocketStartConnect(ctx, handle, 0, IPSocketAddress{}); err != nil {
		t.Fatalf("resume failed connect: %v", err)
	}
	streams, err := host.MethodTCPSocketFinishConnect(context.Background(), handle)
	requireNetworkError(t, err, NetworkErrorConnectionRefused)
	if streams != nil {
		t.Fatalf("failure stream handles = %+v, want none", streams)
	}
	if socket.PendingError() != nil || socket.State() != preview2.TCPStateClosed {
		t.Fatalf("failure state = %d, pending error = %v", socket.State(), socket.PendingError())
	}
	if storedInput, storedOutput := socket.StreamHandles(); storedInput != 0 || storedOutput != 0 {
		t.Fatalf("stored failure stream handles = (%d, %d), want zero", storedInput, storedOutput)
	}
}

func TestS08TCPFinishConnectAdoptsStreams(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPHost(resources)
	socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	socket.SetState(preview2.TCPStateConnectInProgress)
	handle := resources.Add(socket)

	left, right := net.Pipe()
	deadline := time.Now().Add(2 * time.Second)
	if err := left.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := right.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	conn := &closeCountingConn{Conn: left}
	t.Cleanup(func() { _ = right.Close() })
	ctx := rewindContext(t, &socketapi.ConnectResult{Conn: conn})

	if err := host.MethodTCPSocketStartConnect(ctx, handle, 0, IPSocketAddress{}); err != nil {
		t.Fatalf("resume successful connect: %v", err)
	}
	streams, networkErr := host.MethodTCPSocketFinishConnect(context.Background(), handle)
	if networkErr != nil {
		t.Fatalf("finish connect: %v", networkErr)
	}
	if streams == nil {
		t.Fatal("finish connect returned no streams")
	}
	inputHandle, outputHandle := streams.Input, streams.Output
	if inputHandle == 0 || outputHandle == 0 || inputHandle == outputHandle {
		t.Fatalf("stream handles = (%d, %d), want distinct nonzero handles", inputHandle, outputHandle)
	}
	inputResource, ok := resources.Get(inputHandle)
	if !ok {
		t.Fatal("input stream handle not found")
	}
	input, ok := inputResource.(*preview2.TCPInputStreamResource)
	if !ok {
		t.Fatalf("input resource type = %T", inputResource)
	}
	outputResource, ok := resources.Get(outputHandle)
	if !ok {
		t.Fatal("output stream handle not found")
	}
	output, ok := outputResource.(*preview2.TCPOutputStreamResource)
	if !ok {
		t.Fatalf("output resource type = %T", outputResource)
	}

	permit, permitErr := output.CheckWrite()
	if permitErr != nil || permit < 3 {
		t.Fatalf("adopted output permit=%d err=%v", permit, permitErr)
	}
	writeResult := make(chan error, 1)
	go func() { writeResult <- output.Write([]byte("out")) }()
	peerData := make([]byte, 3)
	if _, err := io.ReadFull(right, peerData); err != nil {
		t.Fatalf("read adopted output stream: %v", err)
	}
	if err := <-writeResult; err != nil {
		t.Fatalf("write adopted output stream: %v", err)
	}
	if string(peerData) != "out" {
		t.Fatalf("output payload = %q, want out", peerData)
	}
	peerWrite := make(chan error, 1)
	go func() {
		_, err := right.Write([]byte("in"))
		peerWrite <- err
	}()
	select {
	case <-input.Notify():
	case <-time.After(time.Second):
		t.Fatal("adopted input did not become ready")
	}
	inputData, err := input.Read(2)
	if err != nil {
		t.Fatalf("read adopted input stream: %v", err)
	}
	if err := <-peerWrite; err != nil {
		t.Fatalf("write input peer: %v", err)
	}
	if string(inputData) != "in" {
		t.Fatalf("input payload = %q, want in", inputData)
	}

	host.ResourceDropTCPSocket(context.Background(), handle)
	resources.Remove(inputHandle)
	resources.Remove(outputHandle)
	if conn.closes.Load() != 1 {
		t.Fatalf("adopted connection close count = %d, want 1", conn.closes.Load())
	}
}

func TestS09TCPDropRejectsLateConnect(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPHost(resources)
	socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	socket.SetState(preview2.TCPStateConnectInProgress)
	handle := resources.Add(socket)

	left, right := net.Pipe()
	lateConn := &closeCountingConn{Conn: left}
	t.Cleanup(func() { _ = right.Close() })
	host.ResourceDropTCPSocket(context.Background(), handle)
	ctx := rewindContext(t, &socketapi.ConnectResult{Conn: lateConn})

	err := host.MethodTCPSocketStartConnect(ctx, handle, 0, IPSocketAddress{})
	requireNetworkError(t, err, NetworkErrorInvalidArgument)
	if lateConn.closes.Load() != 1 {
		t.Fatalf("late connection close count = %d, want 1", lateConn.closes.Load())
	}
	if _, ok := resources.Get(handle); ok {
		t.Fatal("dropped socket was reinstalled")
	}
}

func TestS10TCPListenAcceptDropOwnership(t *testing.T) {
	resources := preview2.NewResourceTableWithLimits(64, 3)
	defer resources.Close()
	host := NewTCPHost(resources)
	rawListener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &closeCountingListener{Listener: rawListener}
	parent := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	parent.SetState(preview2.TCPStateListenInProgress)
	parent.SetListener(listener)
	parentHandle := resources.Add(parent)
	if err := host.MethodTCPSocketFinishListen(t.Context(), parentHandle); err != nil {
		t.Fatal(err)
	}
	subscription := host.MethodTCPSocketSubscribe(t.Context(), parentHandle)
	raw, _ := resources.Get(subscription)
	pollable := raw.(preview2.Pollable)
	if pollable.Ready() {
		t.Fatal("empty listener falsely ready")
	}
	accepted, networkErr := host.MethodTCPSocketAccept(t.Context(), parentHandle)
	requireNetworkError(t, networkErr, NetworkErrorWouldBlock)
	if accepted != nil {
		t.Fatal("empty accept returned child")
	}
	client, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "tcp4", rawListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	waitCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	pollable.Block(waitCtx)
	if !pollable.Ready() {
		t.Fatal("listener did not become ready")
	}
	resources.Remove(subscription)
	if listener.closes.Load() != 0 {
		t.Fatal("subscription drop closed listener")
	}
	accepted, networkErr = host.MethodTCPSocketAccept(t.Context(), parentHandle)
	if networkErr != nil || accepted == nil {
		t.Fatalf("accept: %v / %v", accepted, networkErr)
	}
	if resources.SocketBudget().Used() > 3 {
		t.Fatal("accepted socket bypassed budget")
	}
	childRaw, _ := resources.Get(accepted.Socket)
	child := childRaw.(*preview2.TCPSocketResource)
	if child.Conn() == nil || child.State() != preview2.TCPStateConnected {
		t.Fatal("child not connected")
	}
	_, networkErr = host.MethodTCPSocketAccept(t.Context(), parentHandle)
	requireNetworkError(t, networkErr, NetworkErrorWouldBlock)
	host.ResourceDropTCPSocket(t.Context(), parentHandle)
	if listener.closes.Load() != 1 {
		t.Fatalf("listener closes=%d", listener.closes.Load())
	}
	if resources.SocketBudget().Used() != 1 {
		t.Fatalf("parent drop affected adopted child or retained reservation: %d", resources.SocketBudget().Used())
	}
	host.ResourceDropTCPSocket(t.Context(), accepted.Socket)
	resources.Remove(accepted.Input)
	resources.Remove(accepted.Output)
	if resources.SocketBudget().Used() != 0 {
		t.Fatal("socket quota retained")
	}
}

func TestTCPAcceptRollsBackPartialHandlePublication(t *testing.T) {
	resources := preview2.NewResourceTableWithLimits(2, 3)
	defer resources.Close()
	host := NewTCPHost(resources)
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	parent := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	parent.SetState(preview2.TCPStateListenInProgress)
	parent.SetListener(listener)
	handle := resources.Add(parent)
	if err := host.MethodTCPSocketFinishListen(t.Context(), handle); err != nil {
		t.Fatal(err)
	}
	client, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	waitCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	parent.AcceptQueue().Block(waitCtx)
	accepted, networkErr := host.MethodTCPSocketAccept(t.Context(), handle)
	requireNetworkError(t, networkErr, NetworkErrorOutOfMemory)
	if accepted != nil {
		t.Fatal("published partial accepted tuple")
	}
	resources.Remove(handle)
	if resources.SocketBudget().Used() != 0 {
		t.Fatal("failed accept leaked quota")
	}
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if _, err := client.Read(b[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("failed child was not closed: %v", err)
	}
}

func TestTCPListenBacklogValidation(t *testing.T) {
	table := preview2.NewResourceTable()
	defer table.Close()
	host := NewTCPHost(table)
	socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	handle := table.Add(socket)
	requireNetworkError(t, host.MethodTCPSocketSetListenBacklogSize(t.Context(), handle, 0), NetworkErrorInvalidArgument)
	if err := host.MethodTCPSocketSetListenBacklogSize(t.Context(), handle, 1); err != nil {
		t.Fatal(err)
	}
	socket.SetState(preview2.TCPStateConnected)
	requireNetworkError(t, host.MethodTCPSocketSetListenBacklogSize(t.Context(), handle, 1), NetworkErrorInvalidState)
}
