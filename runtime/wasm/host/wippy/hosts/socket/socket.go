// SPDX-License-Identifier: MPL-2.0

// Package socket exposes instance-owned outbound TCP to core WebAssembly
// modules through integer-only imports:
//
//	connect(host_ptr, host_len, port, timeout_ms) -> status<<32 | handle
//	send(handle, buf_ptr, buf_len)                -> status<<32 | written
//	recv(handle, out_ptr, out_cap)                -> status<<32 | read
//	close(handle)                                 -> status
package socket

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/tetratelabs/wazero/api"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/resource"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"

	netapi "github.com/wippyai/runtime/api/net"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
)

const Namespace = "wippy:runtime/socket@0.1.0"

const connectionResourceType uint32 = 0x534f434b // "SOCK"
const maxHostBytes = 253

const (
	StatusOK uint32 = iota
	StatusInvalid
	StatusDenied
	StatusFailed
	StatusUnknownHandle
	StatusLimit
	StatusTimeout
)

func Register(rt *wasmrt.Runtime) error {
	i32, i64 := api.ValueTypeI32, api.ValueTypeI64
	register := func(name string, params, results []api.ValueType, fn api.GoModuleFunc) error {
		return rt.RegisterCoreFunc(Namespace, name, params, results, fn, false)
	}
	if err := register("connect", []api.ValueType{i32, i32, i32, i32}, []api.ValueType{i64}, Connect); err != nil {
		return err
	}
	if err := register("send", []api.ValueType{i32, i32, i32}, []api.ValueType{i64}, Send); err != nil {
		return err
	}
	if err := register("recv", []api.ValueType{i32, i32, i32}, []api.ValueType{i64}, Recv); err != nil {
		return err
	}
	return register("close", []api.ValueType{i32}, []api.ValueType{i32}, Close)
}

type leaseConn struct {
	net.Conn
	lease    *preview2.SocketLease
	closeErr error
	once     sync.Once
}

func (l *leaseConn) Close() error {
	l.once.Do(func() {
		defer l.lease.Release()
		if l.Conn != nil {
			l.closeErr = l.Conn.Close()
		}
	})
	return l.closeErr
}

func wrapWithLease(conn net.Conn, lease *preview2.SocketLease) net.Conn {
	if lease == nil {
		return conn
	}
	return &leaseConn{Conn: conn, lease: lease}
}

type connection struct {
	net.Conn
	closeErr error
	once     sync.Once
}

func (c *connection) Drop() {
	_ = c.Close()
}

func (c *connection) Close() error {
	c.once.Do(func() {
		if c.Conn != nil {
			c.closeErr = c.Conn.Close()
		}
	})
	return c.closeErr
}

func pack(status, value uint32) uint64 {
	return uint64(status)<<32 | uint64(value)
}

func resources(ctx context.Context) *resource.UnifiedTable {
	return wasmengine.ResourcesFromContext(ctx)
}

func getConnection(ctx context.Context, handle uint32) (*connection, bool) {
	table := resources(ctx)
	if table == nil {
		return nil, false
	}
	value, ok := table.GetTyped(resource.Handle(handle), connectionResourceType)
	if !ok {
		return nil, false
	}
	conn, ok := value.(*connection)
	return conn, ok
}

func socketTimeout(limits wasmapi.LimitsConfig, requested uint32) time.Duration {
	timeout := limits.EffectiveSocketTimeout()
	if requested > 0 {
		return min(timeout, time.Duration(requested)*time.Millisecond)
	}
	return timeout
}

func boundOperation(ctx context.Context, conn net.Conn, limits wasmapi.LimitsConfig) (context.Context, func()) {
	operationCtx, cancel := context.WithTimeout(ctx, socketTimeout(limits, 0))
	stop := context.AfterFunc(operationCtx, func() { _ = conn.Close() })
	return operationCtx, func() {
		stopped := stop()
		deadline, hasDeadline := operationCtx.Deadline()
		// The network deadline can fire before the context timer. Do not stop
		// cancellation cleanup and keep the socket alive in that interval.
		if !stopped || operationCtx.Err() != nil || (hasDeadline && !time.Now().Before(deadline)) {
			_ = conn.Close()
		}
		cancel()
		_ = conn.SetDeadline(time.Time{})
	}
}

func Connect(ctx context.Context, mod api.Module, stack []uint64) {
	hostPtr, hostLen := uint32(stack[0]), uint32(stack[1])
	port, timeoutMS := uint32(stack[2]), uint32(stack[3])
	if hostLen == 0 || hostLen > maxHostBytes || port == 0 || port > 65535 {
		stack[0] = pack(StatusInvalid, 0)
		return
	}
	hostBytes, ok := mod.Memory().Read(hostPtr, hostLen)
	if !ok {
		stack[0] = pack(StatusInvalid, 0)
		return
	}

	table := resources(ctx)
	if table == nil {
		stack[0] = pack(StatusFailed, 0)
		return
	}
	limits := wippyhost.GetCallLimits(ctx)
	budget := wippyhost.GetSocketBudget(ctx)
	var lease *preview2.SocketLease
	if budget != nil {
		var err error
		lease, err = budget.Acquire()
		if err != nil {
			stack[0] = pack(StatusLimit, 0)
			return
		}
	} else if table.Count(connectionResourceType) >= limits.EffectiveMaxOpenSockets() {
		stack[0] = pack(StatusLimit, 0)
		return
	}

	transferred := false
	defer func() {
		if !transferred {
			lease.Release()
		}
	}()

	dialCtx, cancel := context.WithTimeout(ctx, socketTimeout(limits, timeoutMS))
	defer cancel()

	service := netapi.GetService(ctx)
	if service == nil {
		stack[0] = pack(StatusFailed, 0)
		return
	}
	conn, err := service.DialContext(dialCtx, "tcp", net.JoinHostPort(string(hostBytes), strconv.Itoa(int(port))))
	if err != nil {
		switch {
		case errors.Is(err, netapi.ErrAccessDenied):
			stack[0] = pack(StatusDenied, 0)
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			stack[0] = pack(StatusTimeout, 0)
		default:
			stack[0] = pack(StatusFailed, 0)
		}
		return
	}
	if err := dialCtx.Err(); err != nil {
		_ = conn.Close()
		stack[0] = pack(StatusTimeout, 0)
		return
	}

	c := &connection{
		Conn: wrapWithLease(conn, lease),
	}
	handle := table.Insert(connectionResourceType, c)
	if handle == 0 {
		_ = c.Close()
		stack[0] = pack(StatusFailed, 0)
		return
	}
	transferred = true
	stack[0] = pack(StatusOK, uint32(handle))
}

func Send(ctx context.Context, mod api.Module, stack []uint64) {
	conn, ok := getConnection(ctx, uint32(stack[0]))
	if !ok {
		stack[0] = pack(StatusUnknownHandle, 0)
		return
	}
	payload, ok := mod.Memory().Read(uint32(stack[1]), uint32(stack[2]))
	if !ok {
		stack[0] = pack(StatusInvalid, 0)
		return
	}
	operationCtx, finish := boundOperation(ctx, conn, wippyhost.GetCallLimits(ctx))
	defer finish()
	if deadline, ok := operationCtx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
	}
	written, err := conn.Write(payload)
	if err != nil {
		stack[0] = pack(statusForIOError(operationCtx, err), uint32(written))
		return
	}
	stack[0] = pack(StatusOK, uint32(written))
}

func Recv(ctx context.Context, mod api.Module, stack []uint64) {
	conn, ok := getConnection(ctx, uint32(stack[0]))
	if !ok {
		stack[0] = pack(StatusUnknownHandle, 0)
		return
	}
	buffer, ok := mod.Memory().Read(uint32(stack[1]), uint32(stack[2]))
	if !ok || len(buffer) == 0 {
		stack[0] = pack(StatusInvalid, 0)
		return
	}
	operationCtx, finish := boundOperation(ctx, conn, wippyhost.GetCallLimits(ctx))
	defer finish()
	if deadline, ok := operationCtx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	}
	read, err := conn.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		stack[0] = pack(statusForIOError(operationCtx, err), uint32(read))
		return
	}
	stack[0] = pack(StatusOK, uint32(read))
}

func statusForIOError(ctx context.Context, err error) uint32 {
	if ctx.Err() != nil {
		return StatusTimeout
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return StatusTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return StatusTimeout
	}
	return StatusFailed
}

func Close(ctx context.Context, _ api.Module, stack []uint64) {
	table := resources(ctx)
	if table == nil {
		stack[0] = uint64(StatusUnknownHandle)
		return
	}
	value, ok := table.GetTyped(resource.Handle(uint32(stack[0])), connectionResourceType)
	if !ok {
		stack[0] = uint64(StatusUnknownHandle)
		return
	}
	if _, ok := value.(*connection); !ok {
		stack[0] = uint64(StatusUnknownHandle)
		return
	}
	table.Remove(resource.Handle(uint32(stack[0])))
	stack[0] = uint64(StatusOK)
}
