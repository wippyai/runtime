// SPDX-License-Identifier: MPL-2.0
package sockets

import (
	"context"
	"errors"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// MethodTCPSocketAccept never waits for network I/O. Empty queues report the
// standard would-block result; a socket subscription wakes when a child arrives.
func (h *TCPHost) MethodTCPSocketAccept(_ context.Context, self uint32) (*TCPAccepted, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return nil, err
	}
	queue := socket.AcceptQueue()
	if socket.State() != preview2.TCPStateListening || queue == nil {
		return nil, &NetworkError{Code: NetworkErrorInvalidState}
	}
	conn, lease, acceptErr := queue.TryAccept()
	if acceptErr != nil {
		if errors.Is(acceptErr, preview2.ErrTCPAcceptWouldBlock) {
			return nil, &NetworkError{Code: NetworkErrorWouldBlock}
		}
		return nil, mapNetError(acceptErr)
	}
	child := preview2.NewTCPSocketResource(socket.Family())
	child.SetState(preview2.TCPStateConnected)
	child.SetConn(conn)
	if local := SocketAddressFromNetAddr(conn.LocalAddr()); local != nil {
		child.SetLocalAddr(local.IPString(), local.Port())
	}
	if remote := SocketAddressFromNetAddr(conn.RemoteAddr()); remote != nil {
		child.SetRemoteAddr(remote.IPString(), remote.Port())
	}
	var handle uint32
	var addErr error
	if lease == nil {
		handle, addErr = h.resources.TryAdd(child)
	} else {
		handle, addErr = h.resources.TryAddWithSocketLease(child, lease)
	}
	if addErr != nil {
		child.Drop()
		lease.Release()
		return nil, resourceLimitError(addErr)
	}
	input := preview2.NewTCPInputStreamResource(child)
	inputHandle, addErr := h.resources.TryAdd(input)
	if addErr != nil {
		input.Drop()
		h.resources.Remove(handle)
		return nil, resourceLimitError(addErr)
	}
	output := preview2.NewTCPOutputStreamResource(child)
	outputHandle, addErr := h.resources.TryAdd(output)
	if addErr != nil {
		output.Drop()
		h.resources.Remove(inputHandle)
		h.resources.Remove(handle)
		return nil, resourceLimitError(addErr)
	}
	child.SetStreamHandles(inputHandle, outputHandle)
	return &TCPAccepted{Socket: handle, Input: inputHandle, Output: outputHandle}, nil
}
