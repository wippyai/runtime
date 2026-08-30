// SPDX-License-Identifier: MPL-2.0

// Package socket exposes outbound TCP to core WebAssembly modules.
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

type connection struct {
	net.Conn
	once sync.Once
}

func (c *connection) Drop() {
	c.once.Do(func() { _ = c.Close() })
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

func operationDeadline(ctx context.Context, limits wasmapi.LimitsConfig) time.Time {
	deadline := time.Now().Add(socketTimeout(limits, 0))
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		return contextDeadline
	}
	return deadline
}

func socketTimeout(limits wasmapi.LimitsConfig, requested uint32) time.Duration {
	milliseconds := int64(limits.EffectiveSocketTimeoutMS())
	if requested > 0 && int64(requested) < milliseconds {
		milliseconds = int64(requested)
	}
	maxMilliseconds := int64(^uint64(0)>>1) / int64(time.Millisecond)
	if milliseconds > maxMilliseconds {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(milliseconds) * time.Millisecond
}

func interruptOnCancel(ctx context.Context, conn net.Conn) func() {
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	return func() {
		stop()
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
	if table.Count(connectionResourceType) >= limits.EffectiveMaxOpenSockets() {
		stack[0] = pack(StatusLimit, 0)
		return
	}

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

	handle := table.Insert(connectionResourceType, &connection{Conn: conn})
	if handle == 0 {
		_ = conn.Close()
		stack[0] = pack(StatusFailed, 0)
		return
	}
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
	_ = conn.SetWriteDeadline(operationDeadline(ctx, wippyhost.GetCallLimits(ctx)))
	defer interruptOnCancel(ctx, conn)()
	written, err := conn.Write(payload)
	if err != nil {
		stack[0] = pack(statusForIOError(ctx, err), uint32(written))
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
	_ = conn.SetReadDeadline(operationDeadline(ctx, wippyhost.GetCallLimits(ctx)))
	defer interruptOnCancel(ctx, conn)()
	read, err := conn.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		stack[0] = pack(statusForIOError(ctx, err), uint32(read))
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
