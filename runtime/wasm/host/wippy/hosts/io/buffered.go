// SPDX-License-Identifier: MPL-2.0
package io

import (
	"context"
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/dispatcher"
	socketapi "github.com/wippyai/runtime/api/socket"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type bufferedOutput interface {
	Write([]byte) error
	CheckWrite() (uint64, error)
	Flush() error
	Subscribe() preview2.Pollable
}
type outputCompletion struct{ err *preview2.StreamError }
type outputPending struct {
	stream    bufferedOutput
	remaining []byte
	flushing  bool
}

func (*outputPending) CmdID() wasmengine.CommandID {
	return wasmengine.CommandID(socketapi.SocketStreamWait)
}
func (*outputPending) Execute(context.Context) (uint64, error) {
	return 0, errors.New("stream operation requires scheduler dispatch")
}
func (p *outputPending) ToCommand() dispatcher.Command { return &socketapi.StreamWaitCmd{Run: p.run} }
func streamFailure(err error) *preview2.StreamError {
	var e *preview2.StreamError
	if errors.As(err, &e) {
		return e
	}
	return &preview2.StreamError{LastOpFailed: true}
}
func (p *outputPending) step() (bool, *preview2.StreamError) {
	if !p.flushing {
		for len(p.remaining) > 0 {
			permit, err := p.stream.CheckWrite()
			if err != nil {
				return true, streamFailure(err)
			}
			if permit == 0 {
				return false, nil
			}
			count := min(uint64(len(p.remaining)), permit)
			if err = p.stream.Write(p.remaining[:count]); err != nil {
				return true, streamFailure(err)
			}
			p.remaining = p.remaining[count:]
		}
		p.remaining = nil
		if err := p.stream.Flush(); err != nil {
			return true, streamFailure(err)
		}
		p.flushing = true
	}
	permit, err := p.stream.CheckWrite()
	if err != nil {
		return true, streamFailure(err)
	}
	return permit > 0, nil
}
func (p *outputPending) run(ctx context.Context) any {
	source := p.stream.Subscribe()
	notifier, ok := source.(interface{ Notify() <-chan struct{} })
	if !ok {
		return &outputCompletion{err: &preview2.StreamError{LastOpFailed: true}}
	}
	for {
		if ctx.Err() != nil {
			return &outputCompletion{err: &preview2.StreamError{Closed: true}}
		}
		if done, err := p.step(); done {
			return &outputCompletion{err: err}
		}
		select {
		case <-ctx.Done():
			return &outputCompletion{err: &preview2.StreamError{Closed: true}}
		case <-notifier.Notify():
		}
	}
}
func (h *StreamsHost) blockingOutput(ctx context.Context, self uint32, contents []byte, flushOnly bool) (*preview2.StreamError, bool) {
	async := wasmengine.GetAsyncify(ctx)
	if async != nil && async.IsRewinding(ctx) {
		token, err := wasmengine.Resume(ctx)
		value, ok := wippyhost.GetAsyncValueStore(ctx).Take(token)
		if err != nil {
			panic(fmt.Errorf("stream resume: %w", err))
		}
		result, typed := value.(*outputCompletion)
		if !ok || !typed || result == nil {
			panic("stream completion missing")
		}
		return h.streamError(result.err), true
	}
	resource, ok := h.resources.Get(self)
	if !ok {
		return &preview2.StreamError{Closed: true}, true
	}
	stream, ok := resource.(bufferedOutput)
	if !ok {
		return nil, false
	}
	task := &outputPending{stream: stream, remaining: contents}
	if flushOnly {
		task.remaining = nil
	}
	if done, err := task.step(); done {
		return h.streamError(err), true
	}
	if async == nil {
		panic("blocking output requires asyncify scheduler context")
	}
	// Retain only the unwritten suffix, owning it across guest suspension.
	task.remaining = append([]byte(nil), task.remaining...)
	if err := wasmengine.Suspend(ctx, task); err != nil {
		panic(fmt.Errorf("stream suspend: %w", err))
	}
	return nil, true
}

// Materialize owned WASI error handles on the guest worker, never in a dispatcher
// goroutine where resource-limit traps could escape the host call boundary.
func (h *StreamsHost) streamError(err *preview2.StreamError) *preview2.StreamError {
	if err == nil || !err.LastOpFailed || err.LastOpFailedErr != 0 {
		return err
	}
	handle := h.resources.Add(preview2.NewErrorResource(err.Error()))
	return &preview2.StreamError{LastOpFailed: true, LastOpFailedErr: handle}
}
func (h *StreamsHost) operationFailed() *preview2.StreamError {
	return h.streamError(&preview2.StreamError{LastOpFailed: true})
}
