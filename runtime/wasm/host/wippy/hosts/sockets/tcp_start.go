// SPDX-License-Identifier: MPL-2.0
package sockets

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/dispatcher"
	socketapi "github.com/wippyai/runtime/api/socket"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// The scheduler acknowledgement means the operation has started, not completed.
// Its eventual result stays owned by the socket until finish or resource drop.
func (h *TCPHost) resumeSocketStart(ctx context.Context, self uint32) *NetworkError {
	token, err := wasmengine.Resume(ctx)
	if err != nil {
		panic(fmt.Errorf("tcp start resume: %w", err))
	}
	store := wippyhost.GetAsyncValueStore(ctx)
	if store == nil {
		panic("tcp start: async value store not found")
	}
	value, ok := store.Take(token)
	if !ok {
		panic("tcp start: missing acknowledgement")
	}
	socket, socketErr := h.getSocket(self)
	if socketErr != nil {
		closeAsyncSocketResult(value)
		return socketErr
	}
	ack, ok := value.(*socketapi.StartResult)
	if !ok || ack == nil {
		closeAsyncSocketResult(value)
		failSocketStart(socket)
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	if ack.Err != nil {
		failSocketStart(socket)
		return mapNetError(ack.Err)
	}
	return nil
}

// Keep the pending job attached until cancellation and physical cleanup finish.
// Concurrent socket drop therefore still joins its cleanup before releasing quota.
func failSocketStart(socket *preview2.TCPSocketResource) {
	if pending := socket.PendingOperation(); pending != nil {
		_ = pending.Close()
	}
	switch socket.State() {
	case preview2.TCPStateConnectInProgress:
		_, _ = socket.ResolvePendingConnect()
		socket.SetState(preview2.TCPStateClosed)
	case preview2.TCPStateListenInProgress:
		_, _ = socket.ResolvePendingListen()
		socket.SetState(preview2.TCPStateBound)
	}
	socket.ClearPendingError()
}

type socketStartOp struct{ cmd dispatcher.Command }

func (o *socketStartOp) CmdID() wasmengine.CommandID   { return wasmengine.CommandID(o.cmd.CmdID()) }
func (o *socketStartOp) ToCommand() dispatcher.Command { return o.cmd }
func (*socketStartOp) Execute(context.Context) (uint64, error) {
	return 0, fmt.Errorf("TCP start requires dispatcher")
}
