// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/wippyai/runtime/api/dispatcher"
	socketapi "github.com/wippyai/runtime/api/socket"
	"github.com/wippyai/runtime/runtime/security"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const TCPNamespace = "wasi:sockets/tcp@0.2.0"

// TCPHost implements wasi:sockets/tcp@0.2.0.
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
		"[method]tcp-socket.accept",
	}
}

// IPSocketAddress represents an IP address and port.
type IPSocketAddress struct {
	Address string
	Port    uint16
}

func (a *IPSocketAddress) String() string {
	if a == nil {
		return ""
	}
	return net.JoinHostPort(a.Address, strconv.Itoa(int(a.Port)))
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

	socket.SetLocalAddr(localAddress.Address, localAddress.Port)
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
	async := wasmengine.GetAsyncify(ctx)

	if async != nil && async.IsRewinding(ctx) {
		result, resumeErr := wasmengine.Resume(ctx)
		if resumeErr != nil {
			panic(fmt.Errorf("tcp start-connect resume: %w", resumeErr))
		}

		store := wippyhost.GetAsyncValueStore(ctx)
		if store == nil {
			panic("tcp start-connect: async value store not found")
		}

		data, ok := store.Take(result)
		if !ok {
			panic(fmt.Sprintf("tcp start-connect: token %d not found", result))
		}

		connectResult := data.(*socketapi.ConnectResult)
		socket, err := h.getSocket(self)
		if err != nil {
			if connectResult.Conn != nil {
				_ = connectResult.Conn.Close()
			}
			return err
		}

		if connectResult.Err != nil {
			socket.SetPendingError(connectResult.Err)
			return nil
		}

		socket.SetConn(connectResult.Conn)
		if tcpAddr, ok := connectResult.Conn.LocalAddr().(*net.TCPAddr); ok {
			socket.SetLocalAddr(tcpAddr.IP.String(), uint16(tcpAddr.Port))
		}
		return nil
	}

	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}

	state := socket.State()
	if state != preview2.TCPStateUnbound && state != preview2.TCPStateBound {
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	addr := remoteAddress.String()
	if !security.IsAllowed(ctx, "socket.connect", addr, nil) {
		return &NetworkError{Code: NetworkErrorAccessDenied}
	}

	socket.SetRemoteAddr(remoteAddress.Address, remoteAddress.Port)
	socket.SetState(preview2.TCPStateConnectInProgress)

	op := &connectPendingOp{cmd: &socketapi.ConnectCmd{Network: "tcp", Address: addr}}

	if async == nil {
		panic("tcp start-connect requires asyncify context")
	}

	if suspendErr := wasmengine.Suspend(ctx, op); suspendErr != nil {
		panic(fmt.Errorf("tcp start-connect suspend: %w", suspendErr))
	}

	return nil
}

// [method]tcp-socket.finish-connect
func (h *TCPHost) MethodTCPSocketFinishConnect(_ context.Context, self uint32) (uint32, uint32, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, 0, err
	}

	if socket.State() != preview2.TCPStateConnectInProgress {
		if socket.State() == preview2.TCPStateUnbound || socket.State() == preview2.TCPStateBound {
			return 0, 0, &NetworkError{Code: NetworkErrorNotInProgress}
		}
		return 0, 0, &NetworkError{Code: NetworkErrorInvalidState}
	}

	if pendingErr := socket.PendingError(); pendingErr != nil {
		socket.ClearPendingError()
		socket.SetState(preview2.TCPStateClosed)
		return 0, 0, mapNetError(pendingErr)
	}

	socket.SetState(preview2.TCPStateConnected)

	inputStream := preview2.NewTCPInputStreamResource(socket)
	outputStream := preview2.NewTCPOutputStreamResource(socket)

	inputHandle := h.resources.Add(inputStream)
	outputHandle := h.resources.Add(outputStream)

	socket.SetStreamHandles(inputHandle, outputHandle)

	return inputHandle, outputHandle, nil
}

// [method]tcp-socket.start-listen
func (h *TCPHost) MethodTCPSocketStartListen(ctx context.Context, self uint32) *NetworkError {
	async := wasmengine.GetAsyncify(ctx)

	if async != nil && async.IsRewinding(ctx) {
		result, resumeErr := wasmengine.Resume(ctx)
		if resumeErr != nil {
			panic(fmt.Errorf("tcp start-listen resume: %w", resumeErr))
		}

		store := wippyhost.GetAsyncValueStore(ctx)
		if store == nil {
			panic("tcp start-listen: async value store not found")
		}

		data, ok := store.Take(result)
		if !ok {
			panic(fmt.Sprintf("tcp start-listen: token %d not found", result))
		}

		listenResult := data.(*socketapi.ListenResult)
		socket, err := h.getSocket(self)
		if err != nil {
			if listenResult.Listener != nil {
				_ = listenResult.Listener.Close()
			}
			return err
		}

		if listenResult.Err != nil {
			socket.SetPendingError(listenResult.Err)
			return nil
		}

		socket.SetListener(listenResult.Listener)
		if tcpAddr, ok := listenResult.Listener.Addr().(*net.TCPAddr); ok {
			socket.SetLocalAddr(tcpAddr.IP.String(), uint16(tcpAddr.Port))
		}
		return nil
	}

	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}

	state := socket.State()
	if state != preview2.TCPStateBound {
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	addr := (&IPSocketAddress{Address: socket.LocalAddr(), Port: socket.LocalPort()}).String()
	if !security.IsAllowed(ctx, "socket.listen", addr, nil) {
		return &NetworkError{Code: NetworkErrorAccessDenied}
	}

	socket.SetState(preview2.TCPStateListenInProgress)

	op := &listenPendingOp{cmd: &socketapi.ListenCmd{Network: "tcp", Address: addr}}

	if async == nil {
		panic("tcp start-listen requires asyncify context")
	}

	if suspendErr := wasmengine.Suspend(ctx, op); suspendErr != nil {
		panic(fmt.Errorf("tcp start-listen suspend: %w", suspendErr))
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

	if pendingErr := socket.PendingError(); pendingErr != nil {
		socket.ClearPendingError()
		socket.SetState(preview2.TCPStateBound)
		return mapNetError(pendingErr)
	}

	socket.SetState(preview2.TCPStateListening)
	return nil
}

// [method]tcp-socket.accept
func (h *TCPHost) MethodTCPSocketAccept(ctx context.Context, self uint32) (uint32, uint32, uint32, *NetworkError) {
	async := wasmengine.GetAsyncify(ctx)

	if async != nil && async.IsRewinding(ctx) {
		result, resumeErr := wasmengine.Resume(ctx)
		if resumeErr != nil {
			panic(fmt.Errorf("tcp accept resume: %w", resumeErr))
		}

		store := wippyhost.GetAsyncValueStore(ctx)
		if store == nil {
			panic("tcp accept: async value store not found")
		}

		data, ok := store.Take(result)
		if !ok {
			panic(fmt.Sprintf("tcp accept: token %d not found", result))
		}

		acceptResult := data.(*socketapi.AcceptResult)
		if acceptResult.Err != nil {
			return 0, 0, 0, mapNetError(acceptResult.Err)
		}

		socket, err := h.getSocket(self)
		if err != nil {
			_ = acceptResult.Conn.Close()
			return 0, 0, 0, err
		}

		newSocket := preview2.NewTCPSocketResource(socket.Family())
		newSocket.SetState(preview2.TCPStateConnected)
		newSocket.SetConn(acceptResult.Conn)

		if tcpAddr, ok := acceptResult.Conn.LocalAddr().(*net.TCPAddr); ok {
			newSocket.SetLocalAddr(tcpAddr.IP.String(), uint16(tcpAddr.Port))
		}
		if tcpAddr, ok := acceptResult.Conn.RemoteAddr().(*net.TCPAddr); ok {
			newSocket.SetRemoteAddr(tcpAddr.IP.String(), uint16(tcpAddr.Port))
		}

		socketHandle := h.resources.Add(newSocket)

		inputStream := preview2.NewTCPInputStreamResource(newSocket)
		outputStream := preview2.NewTCPOutputStreamResource(newSocket)

		inputHandle := h.resources.Add(inputStream)
		outputHandle := h.resources.Add(outputStream)

		newSocket.SetStreamHandles(inputHandle, outputHandle)

		return socketHandle, inputHandle, outputHandle, nil
	}

	socket, err := h.getSocket(self)
	if err != nil {
		return 0, 0, 0, err
	}

	if socket.State() != preview2.TCPStateListening {
		return 0, 0, 0, &NetworkError{Code: NetworkErrorInvalidState}
	}

	netListener, ok := socket.Listener().(net.Listener)
	if !ok {
		return 0, 0, 0, &NetworkError{Code: NetworkErrorInvalidState}
	}

	op := &acceptPendingOp{cmd: &socketapi.AcceptCmd{Listener: netListener}}

	if async == nil {
		panic("tcp accept requires asyncify context")
	}

	if suspendErr := wasmengine.Suspend(ctx, op); suspendErr != nil {
		panic(fmt.Errorf("tcp accept suspend: %w", suspendErr))
	}

	return 0, 0, 0, nil
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

	return &IPSocketAddress{
		Address: socket.LocalAddr(),
		Port:    socket.LocalPort(),
	}, nil
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

	return &IPSocketAddress{
		Address: socket.RemoteAddr(),
		Port:    socket.RemotePort(),
	}, nil
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
	socket, _ := h.getSocket(self)

	pollable := &preview2.PollableResource{}
	if socket != nil {
		state := socket.State()
		ready := state == preview2.TCPStateConnected ||
			state == preview2.TCPStateListening ||
			state == preview2.TCPStateClosed ||
			socket.PendingError() != nil ||
			socket.Conn() != nil ||
			socket.Listener() != nil
		pollable.SetReady(ready)
	}
	return h.resources.Add(pollable)
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

type connectPendingOp struct {
	cmd *socketapi.ConnectCmd
}

func (o *connectPendingOp) CmdID() wasmengine.CommandID {
	return wasmengine.CommandID(socketapi.SocketConnect)
}

func (o *connectPendingOp) ToCommand() dispatcher.Command {
	return o.cmd
}

func (o *connectPendingOp) Execute(_ context.Context) (uint64, error) {
	return 0, fmt.Errorf("TCP connect requires dispatcher")
}

type listenPendingOp struct {
	cmd *socketapi.ListenCmd
}

func (o *listenPendingOp) CmdID() wasmengine.CommandID {
	return wasmengine.CommandID(socketapi.SocketListen)
}

func (o *listenPendingOp) ToCommand() dispatcher.Command {
	return o.cmd
}

func (o *listenPendingOp) Execute(_ context.Context) (uint64, error) {
	return 0, fmt.Errorf("TCP listen requires dispatcher")
}

type acceptPendingOp struct {
	cmd *socketapi.AcceptCmd
}

func (o *acceptPendingOp) CmdID() wasmengine.CommandID {
	return wasmengine.CommandID(socketapi.SocketAccept)
}

func (o *acceptPendingOp) ToCommand() dispatcher.Command {
	return o.cmd
}

func (o *acceptPendingOp) Execute(_ context.Context) (uint64, error) {
	return 0, fmt.Errorf("TCP accept requires dispatcher")
}
