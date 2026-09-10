// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"
	"fmt"
	"net"

	socketapi "github.com/wippyai/runtime/api/socket"
	"github.com/wippyai/runtime/runtime/security"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const UDPNamespace = "wasi:sockets/udp@0.2.8"

// UDPHost implements wasi:sockets/udp@0.2.8.
type UDPHost struct {
	resources *preview2.ResourceTable
	permits   map[uint32]udpSendPermit
}

func NewUDPHost(resources *preview2.ResourceTable) *UDPHost {
	return &UDPHost{resources: resources, permits: make(map[uint32]udpSendPermit)}
}

func (h *UDPHost) Namespace() string {
	return UDPNamespace
}

// AsyncFunctions marks methods that use asyncify suspend/resume.
func (h *UDPHost) AsyncFunctions() []string {
	return []string{
		"[method]udp-socket.start-bind",
	}
}

// IncomingDatagram represents an incoming UDP datagram.
type IncomingDatagram struct {
	RemoteAddress IPSocketAddress
	Data          []byte
}

// OutgoingDatagram represents an outgoing UDP datagram.
type OutgoingDatagram struct {
	RemoteAddress *IPSocketAddress
	Data          []byte
}

func (h *UDPHost) getSocket(handle uint32) (*preview2.UDPSocketResource, *NetworkError) {
	r, ok := h.resources.Get(handle)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	socket, ok := r.(*preview2.UDPSocketResource)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	return socket, nil
}

// Start acknowledges dispatch; the socket owns the unfinished bind result.
func (h *UDPHost) MethodUDPSocketStartBind(ctx context.Context, self uint32, network uint32, localAddress IPSocketAddress) *NetworkError {
	if async := wasmengine.GetAsyncify(ctx); async != nil && async.IsRewinding(ctx) {
		return h.resumeUDPBindStart(ctx, self)
	}
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	if socket.State() != preview2.UDPStateUnbound {
		return &NetworkError{Code: NetworkErrorInvalidState}
	}
	if err := ValidateAddressFamily(&localAddress, socket.Family()); err != nil {
		return err
	}
	if err := ValidateFlowInfo(&localAddress); err != nil {
		return err
	}
	resource, ok := h.resources.Get(network)
	if !ok || resource.Type() != preview2.ResourceNetwork {
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	if !security.IsAllowed(ctx, "socket.listen", localAddress.String(), nil) {
		return &NetworkError{Code: NetworkErrorAccessDenied}
	}
	if wasmengine.GetAsyncify(ctx) == nil {
		panic("UDP start-bind requires asyncify context")
	}
	operation := socketapi.NewPendingOperation()
	socket.SetLocalAddr(localAddress.IPString(), localAddress.Port())
	socket.SetState(preview2.UDPStateBindInProgress)
	if err := socket.SetPendingOperation(operation); err != nil {
		_ = operation.Close()
		return mapNetError(err)
	}
	cmd := &socketapi.StartBindCmd{Operation: operation, Network: "udp", Address: localAddress.String(), Timeout: wippyhost.GetCallLimits(ctx).EffectiveSocketTimeout()}
	if err := wasmengine.Suspend(ctx, &socketStartOp{cmd: cmd}); err != nil {
		_ = operation.Close()
		panic(fmt.Errorf("UDP bind suspend: %w", err))
	}
	return nil
}

func (h *UDPHost) resumeUDPBindStart(ctx context.Context, self uint32) *NetworkError {
	token, err := wasmengine.Resume(ctx)
	if err != nil {
		panic(fmt.Errorf("UDP bind resume: %w", err))
	}
	store := wippyhost.GetAsyncValueStore(ctx)
	if store == nil {
		panic("UDP bind acknowledgement store missing")
	}
	value, ok := store.Take(token)
	if !ok {
		panic("UDP bind acknowledgement missing")
	}
	socket, socketErr := h.getSocket(self)
	if socketErr != nil {
		closeAsyncSocketResult(value)
		return socketErr
	}
	ack, valid := value.(*socketapi.StartResult)
	if valid && ack != nil && ack.Err == nil {
		return nil
	}
	closeAsyncSocketResult(value)
	if pending := socket.PendingOperation(); pending != nil {
		_ = pending.Close()
		_, _ = socket.ResolvePendingBind()
	}
	socket.ClearPendingError()
	socket.SetState(preview2.UDPStateUnbound)
	if valid && ack != nil {
		return mapNetError(ack.Err)
	}
	return &NetworkError{Code: NetworkErrorInvalidArgument}
}

func (h *UDPHost) MethodUDPSocketFinishBind(_ context.Context, self uint32) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	if socket.State() != preview2.UDPStateBindInProgress {
		if socket.State() == preview2.UDPStateUnbound {
			return &NetworkError{Code: NetworkErrorNotInProgress}
		}
		return &NetworkError{Code: NetworkErrorInvalidState}
	}
	ready, bindErr := socket.ResolvePendingBind()
	if bindErr != nil {
		socket.ClearPendingError()
		socket.SetState(preview2.UDPStateUnbound)
		return mapNetError(bindErr)
	}
	if !ready {
		return &NetworkError{Code: NetworkErrorWouldBlock}
	}
	socket.SetState(preview2.UDPStateBound)
	return nil
}

// [method]udp-socket.stream
func (h *UDPHost) MethodUDPSocketStream(ctx context.Context, self uint32, remoteAddress *IPSocketAddress) (uint32, uint32, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, 0, err
	}

	if socket.State() != preview2.UDPStateBound {
		return 0, 0, &NetworkError{Code: NetworkErrorInvalidState}
	}

	var remoteAddr string
	var remotePort uint16
	if remoteAddress != nil {
		if err := validateUDPRemote(remoteAddress, socket.Family()); err != nil {
			return 0, 0, err
		}
		if err := authorizeUDPDestination(ctx, remoteAddress); err != nil {
			return 0, 0, err
		}
		remoteAddr, remotePort = remoteAddress.IPString(), remoteAddress.Port()
	}
	// WASI permits trapping if a previous stream pair has not been dropped.
	oldIn, oldOut := socket.StreamHandles()
	if prior, ok := h.resources.Get(oldIn); ok {
		if stream, typed := prior.(*preview2.IncomingDatagramStreamResource); typed && stream.Socket() == socket {
			panic("UDP stream requires dropping previous streams")
		}
	}
	if prior, ok := h.resources.Get(oldOut); ok {
		if stream, typed := prior.(*preview2.OutgoingDatagramStreamResource); typed && stream.Socket() == socket {
			panic("UDP stream requires dropping previous streams")
		}
	}
	incomingStream := preview2.NewIncomingDatagramStreamResource(socket, remoteAddr, remotePort)
	outgoingStream := preview2.NewOutgoingDatagramStreamResource(socket, remoteAddr, remotePort)
	incomingHandle, addErr := h.resources.TryAdd(incomingStream)
	if addErr != nil {
		incomingStream.Drop()
		outgoingStream.Drop()
		return 0, 0, resourceLimitError(addErr)
	}
	outgoingHandle, addErr := h.resources.TryAdd(outgoingStream)
	if addErr != nil {
		h.resources.Remove(incomingHandle)
		outgoingStream.Drop()
		return 0, 0, resourceLimitError(addErr)
	}
	socket.SetRemoteAddr(remoteAddr, remotePort)
	socket.SetStreamHandles(incomingHandle, outgoingHandle)
	return incomingHandle, outgoingHandle, nil
}

// [method]udp-socket.address-family
func (h *UDPHost) MethodUDPSocketAddressFamily(_ context.Context, self uint32) (uint8, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.Family(), nil
}

// [method]udp-socket.local-address
func (h *UDPHost) MethodUDPSocketLocalAddress(_ context.Context, self uint32) (*IPSocketAddress, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return nil, err
	}

	if socket.State() != preview2.UDPStateBound {
		return nil, &NetworkError{Code: NetworkErrorInvalidState}
	}

	addr := SocketAddressFromHostPort(socket.LocalAddr(), socket.LocalPort())
	if addr == nil || ValidateAddressFamily(addr, socket.Family()) != nil {
		return nil, &NetworkError{Code: NetworkErrorUnknown}
	}
	return addr, nil
}

// [method]udp-socket.remote-address
func (h *UDPHost) MethodUDPSocketRemoteAddress(_ context.Context, self uint32) (*IPSocketAddress, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return nil, err
	}

	if socket.RemoteAddr() == "" {
		return nil, &NetworkError{Code: NetworkErrorInvalidState}
	}

	addr := SocketAddressFromHostPort(socket.RemoteAddr(), socket.RemotePort())
	if addr == nil || ValidateAddressFamily(addr, socket.Family()) != nil {
		return nil, &NetworkError{Code: NetworkErrorUnknown}
	}
	return addr, nil
}

// [method]udp-socket.subscribe
func (h *UDPHost) MethodUDPSocketSubscribe(_ context.Context, self uint32) uint32 {
	socket, err := h.getSocket(self)
	if err != nil {
		panic("invalid UDP socket subscription")
	}
	return h.addUDPPollable(socket.Subscribe())
}

// [method]udp-socket.receive-buffer-size
func (h *UDPHost) MethodUDPSocketReceiveBufferSize(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.ReceiveBufferSize(), nil
}

// [method]udp-socket.set-receive-buffer-size
func (h *UDPHost) MethodUDPSocketSetReceiveBufferSize(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetReceiveBufferSize(value)
	return nil
}

// [method]udp-socket.send-buffer-size
func (h *UDPHost) MethodUDPSocketSendBufferSize(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.SendBufferSize(), nil
}

// [method]udp-socket.set-send-buffer-size
func (h *UDPHost) MethodUDPSocketSetSendBufferSize(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetSendBufferSize(value)
	return nil
}

// [method]udp-socket.unicast-hop-limit
func (h *UDPHost) MethodUDPSocketUnicastHopLimit(_ context.Context, self uint32) (uint8, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.UnicastHopLimit(), nil
}

// [method]udp-socket.set-unicast-hop-limit
func (h *UDPHost) MethodUDPSocketSetUnicastHopLimit(_ context.Context, self uint32, value uint8) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetUnicastHopLimit(value)
	return nil
}

func (h *UDPHost) getIncomingStream(handle uint32) (*preview2.IncomingDatagramStreamResource, *NetworkError) {
	r, ok := h.resources.Get(handle)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	stream, ok := r.(*preview2.IncomingDatagramStreamResource)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	return stream, nil
}

func (h *UDPHost) getOutgoingStream(handle uint32) (*preview2.OutgoingDatagramStreamResource, *NetworkError) {
	r, ok := h.resources.Get(handle)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	stream, ok := r.(*preview2.OutgoingDatagramStreamResource)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	return stream, nil
}

// Guest host calls are serialized by the actor. A permit is tied to the
// resource identity as well as the handle, so recycling a handle cannot reuse it.
type udpSendPermit struct {
	stream *preview2.OutgoingDatagramStreamResource
	count  uint64
}

func (h *UDPHost) MethodIncomingDatagramStreamReceive(_ context.Context, self uint32, maxResults uint64) ([]IncomingDatagram, *NetworkError) {
	stream, err := h.getIncomingStream(self)
	if err != nil {
		return nil, err
	}
	socket := stream.Socket()
	if socket == nil || socket.State() != preview2.UDPStateBound || socket.Conn() == nil {
		return nil, &NetworkError{Code: NetworkErrorInvalidState}
	}
	packets, receiveErr := socket.ReceiveDatagrams(min(maxResults, maxUDPBatch))
	if receiveErr != nil {
		return nil, mapNetError(receiveErr)
	}
	results := make([]IncomingDatagram, 0, len(packets))
	host, port, connected := stream.RemoteAddr()
	var expected *IPSocketAddress
	if connected {
		expected = SocketAddressFromHostPort(host, port)
	}
	for _, packet := range packets {
		remote := SocketAddressFromNetAddr(packet.Address)
		if remote == nil {
			return nil, &NetworkError{Code: NetworkErrorUnknown}
		}
		if connected && (expected == nil || !remote.Equal(expected)) {
			continue
		}
		results = append(results, IncomingDatagram{Data: packet.Data, RemoteAddress: *remote})
	}
	return results, nil
}

func (h *UDPHost) MethodIncomingDatagramStreamSubscribe(_ context.Context, self uint32) uint32 {
	stream, err := h.getIncomingStream(self)
	if err != nil {
		panic("invalid incoming datagram stream")
	}
	return h.addUDPPollable(stream.Pollable())
}

func (h *UDPHost) MethodOutgoingDatagramStreamCheckSend(_ context.Context, self uint32) (uint64, *NetworkError) {
	delete(h.permits, self)
	stream, err := h.getOutgoingStream(self)
	if err != nil {
		return 0, err
	}
	socket := stream.Socket()
	if socket == nil || socket.State() != preview2.UDPStateBound || socket.Conn() == nil {
		return 0, &NetworkError{Code: NetworkErrorInvalidState}
	}
	count, checkErr := socket.CheckSend()
	if checkErr != nil {
		return 0, mapNetError(checkErr)
	}
	count = min(count, maxUDPBatch)
	h.permits[self] = udpSendPermit{stream: stream, count: count}
	return count, nil
}

func (h *UDPHost) MethodOutgoingDatagramStreamSend(ctx context.Context, self uint32, datagrams []OutgoingDatagram) (uint64, *NetworkError) {
	stream, err := h.getOutgoingStream(self)
	if err != nil {
		return 0, err
	}
	permit, permitted := h.permits[self]
	delete(h.permits, self)
	if !permitted || permit.stream != stream || uint64(len(datagrams)) > permit.count {
		panic("UDP send requires a sufficient check-send permit")
	}
	socket := stream.Socket()
	if socket == nil || socket.State() != preview2.UDPStateBound || socket.Conn() == nil {
		return 0, &NetworkError{Code: NetworkErrorInvalidState}
	}
	defaultHost, defaultPort, connected := stream.RemoteAddr()
	var destination *IPSocketAddress
	if connected {
		destination = SocketAddressFromHostPort(defaultHost, defaultPort)
	}
	var sent uint64
	for _, datagram := range datagrams {
		address := datagram.RemoteAddress
		if address == nil {
			address = destination
		}
		validation := validateUDPRemote(address, socket.Family())
		if validation == nil && connected && (destination == nil || !address.Equal(destination)) {
			validation = &NetworkError{Code: NetworkErrorInvalidArgument}
		}
		if validation == nil && len(datagram.Data) > maxUDPDatagramBytes {
			validation = &NetworkError{Code: NetworkErrorDatagramTooLarge}
		}
		if validation == nil {
			validation = authorizeUDPDestination(ctx, address)
		}
		if validation != nil {
			if sent > 0 {
				return sent, nil
			}
			return 0, validation
		}
		addr := &net.UDPAddr{IP: address.IP(), Port: int(address.Port())}
		if address.IPv6 != nil {
			addr.Zone = ZoneFromScopeID(address.IPv6.ScopeID)
		}
		count, sendErr := socket.SendDatagrams([]preview2.UDPDatagram{{Data: datagram.Data, Address: addr}})
		sent += count
		if sendErr != nil {
			if sent > 0 {
				return sent, nil
			}
			return 0, mapNetError(sendErr)
		}
		if count == 0 {
			break
		}
	}
	return sent, nil
}

func validateUDPRemote(address *IPSocketAddress, family uint8) *NetworkError {
	if address == nil {
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	if err := ValidateAddressFamily(address, family); err != nil {
		return err
	}
	if err := ValidateFlowInfo(address); err != nil {
		return err
	}
	if address.Port() == 0 || address.IP() == nil || address.IP().IsUnspecified() {
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	return nil
}

func (h *UDPHost) MethodOutgoingDatagramStreamSubscribe(_ context.Context, self uint32) uint32 {
	stream, err := h.getOutgoingStream(self)
	if err != nil {
		panic("invalid outgoing datagram stream")
	}
	return h.addUDPPollable(stream.Pollable())
}

// ResourceDropUDPSocket drops a UDP socket resource.
func (h *UDPHost) ResourceDropUDPSocket(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// ResourceDropIncomingDatagramStream drops an incoming datagram stream resource.
func (h *UDPHost) ResourceDropIncomingDatagramStream(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// ResourceDropOutgoingDatagramStream drops an outgoing datagram stream resource.
func (h *UDPHost) ResourceDropOutgoingDatagramStream(_ context.Context, self uint32) {
	delete(h.permits, self)
	h.resources.Remove(self)
}

func (h *UDPHost) Register() map[string]any {
	return map[string]any{
		"[method]udp-socket.start-bind":  h.MethodUDPSocketStartBind,
		"[method]udp-socket.finish-bind": h.MethodUDPSocketFinishBind,
		"[method]udp-socket.stream": func(ctx context.Context, self uint32, remote *IPSocketAddress) (*UDPStreams, *NetworkError) {
			incoming, outgoing, err := h.MethodUDPSocketStream(ctx, self, remote)
			if err != nil {
				return nil, err
			}
			return &UDPStreams{Incoming: incoming, Outgoing: outgoing}, nil
		},
		"[method]udp-socket.address-family":           h.MethodUDPSocketAddressFamily,
		"[method]udp-socket.local-address":            h.MethodUDPSocketLocalAddress,
		"[method]udp-socket.remote-address":           h.MethodUDPSocketRemoteAddress,
		"[method]udp-socket.subscribe":                h.MethodUDPSocketSubscribe,
		"[method]udp-socket.receive-buffer-size":      h.MethodUDPSocketReceiveBufferSize,
		"[method]udp-socket.set-receive-buffer-size":  h.MethodUDPSocketSetReceiveBufferSize,
		"[method]udp-socket.send-buffer-size":         h.MethodUDPSocketSendBufferSize,
		"[method]udp-socket.set-send-buffer-size":     h.MethodUDPSocketSetSendBufferSize,
		"[method]udp-socket.unicast-hop-limit":        h.MethodUDPSocketUnicastHopLimit,
		"[method]udp-socket.set-unicast-hop-limit":    h.MethodUDPSocketSetUnicastHopLimit,
		"[resource-drop]udp-socket":                   h.ResourceDropUDPSocket,
		"[method]incoming-datagram-stream.receive":    h.MethodIncomingDatagramStreamReceive,
		"[method]incoming-datagram-stream.subscribe":  h.MethodIncomingDatagramStreamSubscribe,
		"[resource-drop]incoming-datagram-stream":     h.ResourceDropIncomingDatagramStream,
		"[method]outgoing-datagram-stream.check-send": h.MethodOutgoingDatagramStreamCheckSend,
		"[method]outgoing-datagram-stream.send":       wasmengine.CheckedHostFunction{Handler: h.MethodOutgoingDatagramStreamSend, Validate: validateUDPDatagrams},
		"[method]outgoing-datagram-stream.subscribe":  h.MethodOutgoingDatagramStreamSubscribe,
		"[resource-drop]outgoing-datagram-stream":     h.ResourceDropOutgoingDatagramStream,
	}
}

// UDPStreams is the single tuple payload of result<tuple<incoming, outgoing>, error-code>.
type UDPStreams struct {
	Incoming uint32
	Outgoing uint32
}

func (h *UDPHost) addUDPPollable(p preview2.Pollable) uint32 {
	handle, err := h.resources.TryAdd(p)
	if err != nil {
		p.Drop()
		panic(fmt.Errorf("UDP subscription: %w", err))
	}
	return handle
}
