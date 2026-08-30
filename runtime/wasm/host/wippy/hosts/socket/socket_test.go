// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	netapi "github.com/wippyai/runtime/api/net"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	secapi "github.com/wippyai/runtime/api/security"
	wippyengine "github.com/wippyai/runtime/runtime/wasm/engine"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	netsystem "github.com/wippyai/runtime/system/net"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

const socketTestWIT = `
connect: func() -> u64;
connect-len: func(length: u32) -> u64;
send: func(handle: u32) -> u64;
recv: func(handle: u32) -> u64;
close: func(handle: u32) -> u32;
`

func socketTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	secapi.SetStrictMode(ctx, false)
	return netapi.WithService(ctx, netsystem.NewSecureService())
}

func socketTestModule(ctx context.Context, t *testing.T, port int) (*wasmrt.Runtime, *wasmrt.Module) {
	t.Helper()
	rt, err := wasmrt.New(ctx)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	if err := Register(rt); err != nil {
		_ = rt.Close(ctx)
		t.Fatalf("register socket host: %v", err)
	}
	source := fmt.Sprintf(`(module
  (import %q "connect" (func $connect (param i32 i32 i32 i32) (result i64)))
  (import %q "send" (func $send (param i32 i32 i32) (result i64)))
  (import %q "recv" (func $recv (param i32 i32 i32) (result i64)))
  (import %q "close" (func $close (param i32) (result i32)))
  (memory (export "memory") 1)
  (data (i32.const 0) "127.0.0.1")
  (data (i32.const 32) "ping")
  (func (export "connect") (result i64)
    i32.const 0 i32.const 9 i32.const %d i32.const 1000 call $connect)
  (func (export "connect-len") (param $length i32) (result i64)
    i32.const 0 local.get $length i32.const %d i32.const 1000 call $connect)
  (func (export "send") (param $handle i32) (result i64)
    local.get $handle i32.const 32 i32.const 4 call $send)
  (func (export "recv") (param $handle i32) (result i64)
    local.get $handle i32.const 64 i32.const 16 call $recv)
  (func (export "close") (param $handle i32) (result i32)
    local.get $handle call $close)
)`, Namespace, Namespace, Namespace, Namespace, port, port)
	module, err := rt.LoadWAT(ctx, source, socketTestWIT)
	if err != nil {
		_ = rt.Close(ctx)
		t.Fatalf("load module: %v", err)
	}
	if err := module.Compile(ctx); err != nil {
		_ = rt.Close(ctx)
		t.Fatalf("compile module: %v", err)
	}
	return rt, module
}

func listenForSocketTest(t *testing.T) (net.Listener, int) {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return listener, listener.Addr().(*net.TCPAddr).Port
}

type recordingNetwork struct {
	peer    net.Conn
	address string
	mu      sync.Mutex
}

func (n *recordingNetwork) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	client, peer := net.Pipe()
	n.mu.Lock()
	n.address = address
	n.peer = peer
	n.mu.Unlock()
	return client, nil
}

func (*recordingNetwork) Listen(context.Context, string, string) (net.Listener, error) {
	return nil, errors.New("not implemented")
}

func (*recordingNetwork) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	return nil, errors.New("not implemented")
}

func (*recordingNetwork) LookupHost(context.Context, string) ([]string, error) {
	return nil, errors.New("not implemented")
}

func (n *recordingNetwork) closePeer() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.peer != nil {
		_ = n.peer.Close()
	}
}

type oneNetworkRegistry struct {
	service netapi.Service
	id      registry.ID
}

func (r oneNetworkRegistry) GetNetwork(id registry.ID) (netapi.Service, error) {
	if id != r.id {
		return nil, netapi.ErrNetworkNotFound
	}
	return r.service, nil
}

func (r oneNetworkRegistry) HasNetwork(id registry.ID) bool { return id == r.id }
func (oneNetworkRegistry) NetworkKind(registry.ID) registry.Kind {
	return netapi.KindSOCKS5
}

func callPacked(ctx context.Context, t *testing.T, inst *wasmrt.Instance, method string, args ...any) (uint32, uint32) {
	t.Helper()
	result, err := inst.Call(ctx, method, args...)
	if err != nil {
		t.Fatalf("call %s: %v", method, err)
	}
	packed, ok := result.(uint64)
	if !ok {
		t.Fatalf("call %s result = %T, want uint64", method, result)
	}
	return uint32(packed >> 32), uint32(packed)
}

func callStatus(ctx context.Context, t *testing.T, inst *wasmrt.Instance, method string, args ...any) uint32 {
	t.Helper()
	result, err := inst.Call(ctx, method, args...)
	if err != nil {
		t.Fatalf("call %s: %v", method, err)
	}
	status, ok := result.(uint32)
	if !ok {
		t.Fatalf("call %s result = %T, want uint32", method, result)
	}
	return status
}

func TestInstanceCloseClosesOwnedConnections(t *testing.T) {
	ctx := socketTestContext()
	listener, port := listenForSocketTest(t)
	defer listener.Close()
	rt, module := socketTestModule(ctx, t, port)
	defer rt.Close(ctx)

	inst, err := module.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	status, _ := callPacked(ctx, t, inst, "connect")
	if status != StatusOK {
		t.Fatalf("connect status = %d, want %d", status, StatusOK)
	}
	peer, err := listener.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer peer.Close()

	if err := inst.Close(ctx); err != nil {
		t.Fatalf("close instance: %v", err)
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, 1)
	if n, err := peer.Read(buffer); n != 0 || err == nil {
		t.Fatalf("peer read after instance close = (%d, %v), want closed connection", n, err)
	}
}

func TestProcessCloseClosesWarmInstanceConnections(t *testing.T) {
	ctx := socketTestContext()
	listener, port := listenForSocketTest(t)
	defer listener.Close()
	rt, module := socketTestModule(ctx, t, port)
	defer rt.Close(ctx)

	proc := wippyengine.NewProcess(module, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	if err := proc.Init(ctx, "connect", nil); err != nil {
		t.Fatalf("init process: %v", err)
	}
	var output processapi.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("step process: %v", err)
	}
	if !output.IsDone() {
		t.Fatalf("process status = %v, want done", output.Status())
	}
	peer, err := listener.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer peer.Close()

	proc.Close()
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, 1)
	if n, err := peer.Read(buffer); n != 0 || err == nil {
		t.Fatalf("peer read after process close = (%d, %v), want closed connection", n, err)
	}
}

func TestConnectUsesNetworkSelectedByCallFrame(t *testing.T) {
	overlay := &recordingNetwork{}
	defer overlay.closePeer()
	networkID := registry.ParseID("app.net:test")
	ctx := socketTestContext()
	ctx = netapi.WithNetworkRegistry(ctx, oneNetworkRegistry{id: networkID, service: overlay})
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	if err := frame.SetMultiple(netapi.DefaultNetworkPair(networkID.String())); err != nil {
		t.Fatalf("select network: %v", err)
	}

	rt, module := socketTestModule(ctx, t, 443)
	defer rt.Close(ctx)
	inst, err := module.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	if status, _ := callPacked(ctx, t, inst, "connect"); status != StatusOK {
		t.Fatalf("connect status = %d", status)
	}
	overlay.mu.Lock()
	address := overlay.address
	overlay.mu.Unlock()
	if address != "127.0.0.1:443" {
		t.Fatalf("overlay address = %q, want 127.0.0.1:443", address)
	}
}

func TestConnectRejectsOversizedHost(t *testing.T) {
	ctx := socketTestContext()
	rt, module := socketTestModule(ctx, t, 443)
	defer rt.Close(ctx)
	inst, err := module.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	if status, _ := callPacked(ctx, t, inst, "connect-len", uint32(maxHostBytes+1)); status != StatusInvalid {
		t.Fatalf("oversized host status = %d, want invalid", status)
	}
}

func TestConnectionLimitIsPerInstance(t *testing.T) {
	base := socketTestContext()
	ctx := wippyhost.WithCallLimits(base, wasmapi.LimitsConfig{MaxOpenSockets: 1})
	listener, port := listenForSocketTest(t)
	defer listener.Close()
	rt, module := socketTestModule(ctx, t, port)
	defer rt.Close(ctx)

	first, err := module.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate first: %v", err)
	}
	defer first.Close(ctx)
	second, err := module.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate second: %v", err)
	}
	defer second.Close(ctx)

	if status, _ := callPacked(ctx, t, first, "connect"); status != StatusOK {
		t.Fatalf("first instance connect status = %d", status)
	}
	if status, _ := callPacked(ctx, t, first, "connect"); status != StatusLimit {
		t.Fatalf("second connection status = %d, want limit", status)
	}
	if status, _ := callPacked(ctx, t, second, "connect"); status != StatusOK {
		t.Fatalf("second instance connect status = %d; limit leaked across instances", status)
	}
}

func TestHandleCannotCloseAnotherInstanceConnection(t *testing.T) {
	ctx := socketTestContext()
	listener, port := listenForSocketTest(t)
	defer listener.Close()
	rt, module := socketTestModule(ctx, t, port)
	defer rt.Close(ctx)

	owner, err := module.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate owner: %v", err)
	}
	defer owner.Close(ctx)
	other, err := module.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate other: %v", err)
	}
	defer other.Close(ctx)

	status, handle := callPacked(ctx, t, owner, "connect")
	if status != StatusOK {
		t.Fatalf("connect status = %d", status)
	}
	peer, err := listener.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer peer.Close()

	if status := callStatus(ctx, t, other, "close", handle); status != StatusUnknownHandle {
		t.Fatalf("foreign close status = %d, want unknown handle", status)
	}
	if status, written := callPacked(ctx, t, owner, "send", handle); status != StatusOK || written != 4 {
		t.Fatalf("owner send after foreign close = (%d, %d), want (ok, 4)", status, written)
	}
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(peer, buffer); err != nil {
		t.Fatalf("read owner payload: %v", err)
	}
	if string(buffer) != "ping" {
		t.Fatalf("owner payload = %q, want ping", buffer)
	}
}

func TestRecvCancellationInterruptsConnection(t *testing.T) {
	ctx := socketTestContext()
	listener, port := listenForSocketTest(t)
	defer listener.Close()
	rt, module := socketTestModule(ctx, t, port)
	defer rt.Close(ctx)
	inst, err := module.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	status, handle := callPacked(ctx, t, inst, "connect")
	if status != StatusOK {
		t.Fatalf("connect status = %d", status)
	}
	peer, err := listener.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer peer.Close()

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	status, _ = callPacked(callCtx, t, inst, "recv", handle)
	if status != StatusTimeout {
		t.Fatalf("recv status = %d, want timeout", status)
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, 1)
	if n, err := peer.Read(buffer); n != 0 || err == nil {
		t.Fatalf("peer read after cancellation = (%d, %v), want closed connection", n, err)
	}
}
