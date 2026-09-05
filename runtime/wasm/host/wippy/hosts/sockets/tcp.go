// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"
	"fmt"
	"net"
	"strconv"

	socketapi "github.com/wippyai/runtime/api/socket"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const TCPNamespace = "wasi:sockets/tcp@0.2.8"

// TCPHost implements wasi:sockets/tcp@0.2.8.
type TCPHost struct {
	resources *preview2.ResourceTable
}

func NewTCPHost(resources *preview2.ResourceTable) *TCPHost {
	return &TCPHost{resources: resources}
}

func (h *TCPHost) Namespace() string {
	return TCPNamespace
}

// AsyncFunctions marks methods that use asyncify suspend/resume.
func (h *TCPHost) AsyncFunctions() []string {
	return []string{
		"[method]tcp-socket.start-connect",
		"[method]tcp-socket.start-listen",
	}
}

type TCPStreams struct {
	Input  uint32
	Output uint32
}

type TCPAccepted struct {
	Socket uint32
	Input  uint32
	Output uint32
}

func closeAsyncSocketResult(value any) {
	switch result := value.(type) {
	case *socketapi.ConnectResult:
		if result != nil && result.Conn != nil {
			_ = result.Conn.Close()
		}
	case *socketapi.ListenResult:
		if result != nil && result.Listener != nil {
			_ = result.Listener.Close()
		}
	case *socketapi.AcceptResult:
		if result != nil && result.Conn != nil {
			_ = result.Conn.Close()
		}
	case *socketapi.BindResult:
		if result != nil && result.Conn != nil {
			_ = result.Conn.Close()
		}
	}
}

func (h *TCPHost) getSocket(handle uint32) (*preview2.TCPSocketResource, *NetworkError) {
	r, ok := h.resources.Get(handle)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	socket, ok := r.(*preview2.TCPSocketResource)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	return socket, nil
}

// [method]tcp-socket.start-bind
func (h *TCPHost) MethodTCPSocketStartBind(_ context.Context, self uint32, _ uint32, localAddress IPSocketAddress) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}

	if socket.State() != preview2.TCPStateUnbound {
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	if err := ValidateAddressFamily(&localAddress, socket.Family()); err != nil {
		return err
	}
	if err := ValidateFlowInfo(&localAddress); err != nil {
		return err
	}

	socket.SetLocalAddr(localAddress.IPString(), localAddress.Port())
	socket.SetState(preview2.TCPStateBindInProgress)

	return nil
}

// [method]tcp-socket.finish-bind
func (h *TCPHost) MethodTCPSocketFinishBind(_ context.Context, self uint32) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}

	if socket.State() != preview2.TCPStateBindInProgress {
		if socket.State() == preview2.TCPStateUnbound {
			return &NetworkError{Code: NetworkErrorNotInProgress}
		}
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	socket.SetState(preview2.TCPStateBound)
	return nil
}

// [method]tcp-socket.start-connect
func (h *TCPHost) MethodTCPSocketStartConnect(ctx context.Context, self uint32, _ uint32, remoteAddress IPSocketAddress) *NetworkError {
	if async := wasmengine.GetAsyncify(ctx); async != nil && async.IsRewinding(ctx) {
		return h.resumeSocketStart(ctx, self)
	}
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	state := socket.State()
	if state != preview2.TCPStateUnbound && state != preview2.TCPStateBound {
		return &NetworkError{Code: NetworkErrorInvalidState}
	}
	if err := ValidateAddressFamily(&remoteAddress, socket.Family()); err != nil {
		return err
	}
	if err := ValidateFlowInfo(&remoteAddress); err != nil {
		return err
	}
	if wasmengine.GetAsyncify(ctx) == nil {
		panic("tcp start-connect requires asyncify context")
	}
	operation := socketapi.NewPendingOperation()
	socket.SetRemoteAddr(remoteAddress.IPString(), remoteAddress.Port())
	socket.SetState(preview2.TCPStateConnectInProgress)
	if err := socket.SetPendingOperation(operation); err != nil {
		operation.Close()
		return mapNetError(err)
	}
	cmd := &socketapi.StartConnectCmd{
		Operation: operation, Network: "tcp", Address: remoteAddress.String(),
		Timeout: wippyhost.GetCallLimits(ctx).EffectiveSocketTimeout(),
	}
	if err := wasmengine.Suspend(ctx, &socketStartOp{cmd: cmd}); err != nil {
		operation.Close()
		panic(fmt.Errorf("tcp start-connect suspend: %w", err))
	}
	return nil
}

// [method]tcp-socket.finish-connect
func (h *TCPHost) MethodTCPSocketFinishConnect(_ context.Context, self uint32) (*TCPStreams, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return nil, err
	}

	if socket.State() != preview2.TCPStateConnectInProgress {
		if socket.State() == preview2.TCPStateUnbound || socket.State() == preview2.TCPStateBound {
			return nil, &NetworkError{Code: NetworkErrorNotInProgress}
		}
		return nil, &NetworkError{Code: NetworkErrorInvalidState}
	}

	ready, completeErr := socket.ResolvePendingConnect()
	if completeErr != nil {
		socket.ClearPendingError()
		socket.SetState(preview2.TCPStateClosed)
		return nil, mapNetError(completeErr)
	}
	if !ready {
		return nil, &NetworkError{Code: NetworkErrorWouldBlock}
	}
	if conn, ok := socket.Conn().(net.Conn); ok {
		if local := SocketAddressFromNetAddr(conn.LocalAddr()); local != nil {
			socket.SetLocalAddr(local.IPString(), local.Port())
		}
	}

	inputStream := preview2.NewTCPInputStreamResource(socket)
	inputHandle, addErr := h.resources.TryAdd(inputStream)
	if addErr != nil {
		inputStream.Drop()
		socket.Drop()
		return nil, resourceLimitError(addErr)
	}
	outputStream := preview2.NewTCPOutputStreamResource(socket)
	outputHandle, addErr := h.resources.TryAdd(outputStream)
	if addErr != nil {
		outputStream.Drop()
		h.resources.Remove(inputHandle)
		socket.Drop()
		return nil, resourceLimitError(addErr)
	}
	socket.SetState(preview2.TCPStateConnected)

	socket.SetStreamHandles(inputHandle, outputHandle)

	return &TCPStreams{Input: inputHandle, Output: outputHandle}, nil
}

// [method]tcp-socket.start-listen
func (h *TCPHost) MethodTCPSocketStartListen(ctx context.Context, self uint32) *NetworkError {
	if async := wasmengine.GetAsyncify(ctx); async != nil && async.IsRewinding(ctx) {
		return h.resumeSocketStart(ctx, self)
	}
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	if socket.State() != preview2.TCPStateBound {
		return &NetworkError{Code: NetworkErrorInvalidState}
	}
	if wasmengine.GetAsyncify(ctx) == nil {
		panic("tcp start-listen requires asyncify context")
	}
	operation := socketapi.NewPendingOperation()
	socket.SetState(preview2.TCPStateListenInProgress)
	if err := socket.SetPendingOperation(operation); err != nil {
		operation.Close()
		return mapNetError(err)
	}
	cmd := &socketapi.StartListenCmd{
		Operation: operation, Network: "tcp", Address: net.JoinHostPort(socket.LocalAddr(), strconv.Itoa(int(socket.LocalPort()))),
		Timeout: wippyhost.GetCallLimits(ctx).EffectiveSocketTimeout(),
	}
	if err := wasmengine.Suspend(ctx, &socketStartOp{cmd: cmd}); err != nil {
		operation.Close()
		panic(fmt.Errorf("tcp start-listen suspend: %w", err))
	}
	return nil
}

// [method]tcp-socket.finish-listen
func (h *TCPHost) MethodTCPSocketFinishListen(_ context.Context, self uint32) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}

	if socket.State() != preview2.TCPStateListenInProgress {
		if socket.State() == preview2.TCPStateBound {
			return &NetworkError{Code: NetworkErrorNotInProgress}
		}
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	ready, completeErr := socket.ResolvePendingListen()
	if completeErr != nil {
		socket.ClearPendingError()
		socket.SetState(preview2.TCPStateBound)
		return mapNetError(completeErr)
	}
	if !ready {
		return &NetworkError{Code: NetworkErrorWouldBlock}
	}

	listener, ok := socket.Listener().(net.Listener)
	if !ok || listener == nil {
		return &NetworkError{Code: NetworkErrorWouldBlock}
	}
	if local := SocketAddressFromNetAddr(listener.Addr()); local != nil {
		socket.SetLocalAddr(local.IPString(), local.Port())
	}
	capacity := int(min(socket.ListenBacklogSize(), uint64(preview2.MaxAcceptQueueCapacity)))
	queue := preview2.NewTCPAcceptQueue(listener, h.resources.SocketBudget(), capacity)
	if err := socket.SetAcceptQueue(queue); err != nil {
		queue.Drop()
		return mapNetError(err)
	}
	socket.SetState(preview2.TCPStateListening)
	return nil
}

// [method]tcp-socket.shutdown
func (h *TCPHost) MethodTCPSocketShutdown(_ context.Context, self uint32, shutdownType uint8) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}

	if socket.State() != preview2.TCPStateConnected {
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	tcpConn, ok := socket.Conn().(*net.TCPConn)
	if !ok {
		return &NetworkError{Code: NetworkErrorNotSupported}
	}

	var shutdownErr error
	switch shutdownType {
	case 0: // Receive
		shutdownErr = tcpConn.CloseRead()
	case 1: // Send
		shutdownErr = tcpConn.CloseWrite()
	case 2: // Both
		shutdownErr = tcpConn.CloseRead()
		if shutdownErr == nil {
			shutdownErr = tcpConn.CloseWrite()
		}
	default:
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	}

	if shutdownErr != nil {
		return mapNetError(shutdownErr)
	}

	return nil
}

// [method]tcp-socket.address-family
func (h *TCPHost) MethodTCPSocketAddressFamily(_ context.Context, self uint32) (uint8, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.Family(), nil
}

// [method]tcp-socket.local-address
func (h *TCPHost) MethodTCPSocketLocalAddress(_ context.Context, self uint32) (*IPSocketAddress, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return nil, err
	}

	state := socket.State()
	if state == preview2.TCPStateUnbound {
		return nil, &NetworkError{Code: NetworkErrorInvalidState}
	}

	addr := SocketAddressFromHostPort(socket.LocalAddr(), socket.LocalPort())
	if addr == nil || ValidateAddressFamily(addr, socket.Family()) != nil {
		return nil, &NetworkError{Code: NetworkErrorUnknown}
	}
	return addr, nil
}

// [method]tcp-socket.remote-address
func (h *TCPHost) MethodTCPSocketRemoteAddress(_ context.Context, self uint32) (*IPSocketAddress, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return nil, err
	}

	if socket.State() != preview2.TCPStateConnected {
		return nil, &NetworkError{Code: NetworkErrorInvalidState}
	}

	addr := SocketAddressFromHostPort(socket.RemoteAddr(), socket.RemotePort())
	if addr == nil || ValidateAddressFamily(addr, socket.Family()) != nil {
		return nil, &NetworkError{Code: NetworkErrorUnknown}
	}
	return addr, nil
}

// [method]tcp-socket.is-listening
func (h *TCPHost) MethodTCPSocketIsListening(_ context.Context, self uint32) bool {
	socket, err := h.getSocket(self)
	if err != nil {
		return false
	}
	return socket.IsListening()
}

// [method]tcp-socket.subscribe
func (h *TCPHost) MethodTCPSocketSubscribe(_ context.Context, self uint32) uint32 {
	socket, err := h.getSocket(self)
	if err != nil {
		panic("tcp subscribe: invalid socket handle")
	}
	return h.resources.Add(socket.Subscribe())
}

// [method]tcp-socket.hop-limit
func (h *TCPHost) MethodTCPSocketHopLimit(_ context.Context, self uint32) (uint8, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.HopLimit(), nil
}

// [method]tcp-socket.set-hop-limit
func (h *TCPHost) MethodTCPSocketSetHopLimit(_ context.Context, self uint32, value uint8) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetHopLimit(value)
	return nil
}

// [method]tcp-socket.receive-buffer-size
func (h *TCPHost) MethodTCPSocketReceiveBufferSize(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.ReceiveBufferSize(), nil
}

// [method]tcp-socket.set-receive-buffer-size
func (h *TCPHost) MethodTCPSocketSetReceiveBufferSize(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetReceiveBufferSize(value)
	return nil
}

// [method]tcp-socket.send-buffer-size
func (h *TCPHost) MethodTCPSocketSendBufferSize(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.SendBufferSize(), nil
}

// [method]tcp-socket.set-send-buffer-size
func (h *TCPHost) MethodTCPSocketSetSendBufferSize(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetSendBufferSize(value)
	return nil
}

// [method]tcp-socket.listen-backlog-size
func (h *TCPHost) MethodTCPSocketListenBacklogSize(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.ListenBacklogSize(), nil
}

// [method]tcp-socket.set-listen-backlog-size
func (h *TCPHost) MethodTCPSocketSetListenBacklogSize(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	if value == 0 {
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	if state := socket.State(); state == preview2.TCPStateConnectInProgress || state == preview2.TCPStateConnected {
		return &NetworkError{Code: NetworkErrorInvalidState}
	}
	socket.SetListenBacklogSize(value)
	return nil
}

// [method]tcp-socket.keep-alive-enabled
func (h *TCPHost) MethodTCPSocketKeepAliveEnabled(_ context.Context, self uint32) (bool, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return false, err
	}
	return socket.KeepAliveEnabled(), nil
}

// [method]tcp-socket.set-keep-alive-enabled
func (h *TCPHost) MethodTCPSocketSetKeepAliveEnabled(_ context.Context, self uint32, value bool) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetKeepAliveEnabled(value)
	return nil
}

// [method]tcp-socket.keep-alive-idle-time
func (h *TCPHost) MethodTCPSocketKeepAliveIdleTime(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.KeepAliveIdleTime(), nil
}

// [method]tcp-socket.set-keep-alive-idle-time
func (h *TCPHost) MethodTCPSocketSetKeepAliveIdleTime(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetKeepAliveIdleTime(value)
	return nil
}

// [method]tcp-socket.keep-alive-interval
func (h *TCPHost) MethodTCPSocketKeepAliveInterval(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.KeepAliveInterval(), nil
}

// [method]tcp-socket.set-keep-alive-interval
func (h *TCPHost) MethodTCPSocketSetKeepAliveInterval(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetKeepAliveInterval(value)
	return nil
}

// [method]tcp-socket.keep-alive-count
func (h *TCPHost) MethodTCPSocketKeepAliveCount(_ context.Context, self uint32) (uint32, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.KeepAliveCount(), nil
}

// [method]tcp-socket.set-keep-alive-count
func (h *TCPHost) MethodTCPSocketSetKeepAliveCount(_ context.Context, self uint32, value uint32) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetKeepAliveCount(value)
	return nil
}

// ResourceDropTCPSocket drops a TCP socket resource.
func (h *TCPHost) ResourceDropTCPSocket(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

func (h *TCPHost) Register() map[string]any {
	return map[string]any{
		"[method]tcp-socket.start-bind":               h.MethodTCPSocketStartBind,
		"[method]tcp-socket.finish-bind":              h.MethodTCPSocketFinishBind,
		"[method]tcp-socket.start-connect":            h.MethodTCPSocketStartConnect,
		"[method]tcp-socket.finish-connect":           h.MethodTCPSocketFinishConnect,
		"[method]tcp-socket.start-listen":             h.MethodTCPSocketStartListen,
		"[method]tcp-socket.finish-listen":            h.MethodTCPSocketFinishListen,
		"[method]tcp-socket.accept":                   h.MethodTCPSocketAccept,
		"[method]tcp-socket.shutdown":                 h.MethodTCPSocketShutdown,
		"[method]tcp-socket.address-family":           h.MethodTCPSocketAddressFamily,
		"[method]tcp-socket.local-address":            h.MethodTCPSocketLocalAddress,
		"[method]tcp-socket.remote-address":           h.MethodTCPSocketRemoteAddress,
		"[method]tcp-socket.is-listening":             h.MethodTCPSocketIsListening,
		"[method]tcp-socket.subscribe":                h.MethodTCPSocketSubscribe,
		"[method]tcp-socket.hop-limit":                h.MethodTCPSocketHopLimit,
		"[method]tcp-socket.set-hop-limit":            h.MethodTCPSocketSetHopLimit,
		"[method]tcp-socket.receive-buffer-size":      h.MethodTCPSocketReceiveBufferSize,
		"[method]tcp-socket.set-receive-buffer-size":  h.MethodTCPSocketSetReceiveBufferSize,
		"[method]tcp-socket.send-buffer-size":         h.MethodTCPSocketSendBufferSize,
		"[method]tcp-socket.set-send-buffer-size":     h.MethodTCPSocketSetSendBufferSize,
		"[method]tcp-socket.listen-backlog-size":      h.MethodTCPSocketListenBacklogSize,
		"[method]tcp-socket.set-listen-backlog-size":  h.MethodTCPSocketSetListenBacklogSize,
		"[method]tcp-socket.keep-alive-enabled":       h.MethodTCPSocketKeepAliveEnabled,
		"[method]tcp-socket.set-keep-alive-enabled":   h.MethodTCPSocketSetKeepAliveEnabled,
		"[method]tcp-socket.keep-alive-idle-time":     h.MethodTCPSocketKeepAliveIdleTime,
		"[method]tcp-socket.set-keep-alive-idle-time": h.MethodTCPSocketSetKeepAliveIdleTime,
		"[method]tcp-socket.keep-alive-interval":      h.MethodTCPSocketKeepAliveInterval,
		"[method]tcp-socket.set-keep-alive-interval":  h.MethodTCPSocketSetKeepAliveInterval,
		"[method]tcp-socket.keep-alive-count":         h.MethodTCPSocketKeepAliveCount,
		"[method]tcp-socket.set-keep-alive-count":     h.MethodTCPSocketSetKeepAliveCount,
		"[resource-drop]tcp-socket":                   h.ResourceDropTCPSocket,
	}
}
