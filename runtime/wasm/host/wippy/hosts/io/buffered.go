// SPDX-License-Identifier: MPL-2.0
package io

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	socketapi "github.com/wippyai/runtime/api/socket"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const streamTimeoutDebug = "stream operation timed out"

type socketAborter interface{ AbortSocket() }

type bufferedOutput interface {
	Write([]byte) error
	CheckWrite() (uint64, error)
	Flush() error
	Subscribe() preview2.Pollable
}

type bufferedInput interface {
	Read(uint64) ([]byte, error)
	Subscribe() preview2.Pollable
}

type outputCompletion struct {
	err *preview2.StreamError
	msg string
}
type inputCompletion struct {
	err  *preview2.StreamError
	msg  string
	data []byte
	n    uint64
}
type spliceCompletion struct {
	err *preview2.StreamError
	msg string
	n   uint64
}

type outputPending struct {
	deadline  time.Time
	stream    bufferedOutput
	remaining []byte
	flushing  bool
}
type inputPending struct {
	stream   bufferedInput
	deadline time.Time
	length   uint64
	skip     bool
}
type splicePending struct {
	src         any
	dst         any
	deadline    time.Time
	remaining   uint64
	transferred uint64
}

func (*outputPending) CmdID() wasmengine.CommandID {
	return wasmengine.CommandID(socketapi.SocketStreamWait)
}
func (*outputPending) Execute(context.Context) (uint64, error) {
	return 0, errors.New("stream operation requires scheduler dispatch")
}
func (p *outputPending) ToCommand() dispatcher.Command {
	return &socketapi.StreamWaitCmd{Run: p.run, Deadline: p.deadline}
}

func (*inputPending) CmdID() wasmengine.CommandID {
	return wasmengine.CommandID(socketapi.SocketStreamWait)
}
func (*inputPending) Execute(context.Context) (uint64, error) {
	return 0, errors.New("stream operation requires scheduler dispatch")
}
func (p *inputPending) ToCommand() dispatcher.Command {
	return &socketapi.StreamWaitCmd{Run: p.run, Deadline: p.deadline}
}

func (*splicePending) CmdID() wasmengine.CommandID {
	return wasmengine.CommandID(socketapi.SocketStreamWait)
}
func (*splicePending) Execute(context.Context) (uint64, error) {
	return 0, errors.New("stream operation requires scheduler dispatch")
}
func (p *splicePending) ToCommand() dispatcher.Command {
	return &socketapi.StreamWaitCmd{Run: p.run, Deadline: p.deadline}
}

func streamFailure(err error) *preview2.StreamError {
	var e *preview2.StreamError
	if errors.As(err, &e) {
		return e
	}
	return &preview2.StreamError{LastOpFailed: true}
}

func socketStreamDeadline(ctx context.Context, streams ...any) time.Time {
	tcp := false
	for _, stream := range streams {
		if _, ok := stream.(socketAborter); ok {
			tcp = true
			break
		}
	}
	if !tcp {
		return time.Time{}
	}
	timeout := wippyhost.GetCallLimits(ctx).EffectiveSocketTimeout()
	if timeout <= 0 {
		return time.Time{}
	}
	return time.Now().Add(timeout)
}

func abortSocket(resource any) {
	if abort, ok := resource.(socketAborter); ok {
		abort.AbortSocket()
	}
}

func waitTerminated(ctx context.Context, streams ...any) (*preview2.StreamError, string) {
	err := ctx.Err()
	if err == nil {
		return nil, ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		for _, stream := range streams {
			abortSocket(stream)
		}
		return &preview2.StreamError{LastOpFailed: true}, streamTimeoutDebug
	}
	return &preview2.StreamError{Closed: true}, ""
}

func waitPollable(ctx context.Context, source preview2.Pollable) error {
	if source == nil {
		return errors.New("pollable has no readiness notification")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if source.Ready() {
		return nil
	}
	notifier, ok := source.(interface{ Notify() <-chan struct{} })
	if !ok {
		return errors.New("pollable has no readiness notification")
	}
	signal := notifier.Notify()
	if signal == nil {
		return errors.New("pollable returned a nil readiness signal")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-signal:
		return nil
	}
}

func streamReady(stream any) bool {
	if ready, ok := stream.(interface{ Ready() bool }); ok {
		return ready.Ready()
	}
	if subscriber, ok := stream.(interface{ Subscribe() preview2.Pollable }); ok {
		return subscriber.Subscribe().Ready()
	}
	return true
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
	for {
		if se, msg := waitTerminated(ctx, p.stream); se != nil {
			return &outputCompletion{err: se, msg: msg}
		}
		if done, err := p.step(); done {
			return &outputCompletion{err: err}
		}
		if err := waitPollable(ctx, source); err != nil {
			if se, msg := waitTerminated(ctx, p.stream); se != nil {
				return &outputCompletion{err: se, msg: msg}
			}
			return &outputCompletion{err: &preview2.StreamError{LastOpFailed: true}}
		}
	}
}

// A readiness wake may yield no data; only data or a terminal error completes
// a nonzero blocking read/skip.
func readReadyInput(stream bufferedInput, length uint64, skip bool) ([]byte, uint64, *preview2.StreamError, bool) {
	if !streamReady(stream) {
		return nil, 0, nil, false
	}
	data, err := stream.Read(length)
	if len(data) == 0 && err == nil {
		return nil, 0, nil, false
	}
	var se *preview2.StreamError
	if err != nil {
		se = streamFailure(err)
	}
	if skip {
		return nil, uint64(len(data)), se, true
	}
	return data, 0, se, true
}

func (p *inputPending) run(ctx context.Context) any {
	source := p.stream.Subscribe()
	for {
		if se, msg := waitTerminated(ctx, p.stream); se != nil {
			return &inputCompletion{err: se, msg: msg}
		}
		if data, n, err, done := readReadyInput(p.stream, p.length, p.skip); done {
			return &inputCompletion{data: data, n: n, err: err}
		}
		if err := waitPollable(ctx, source); err != nil {
			if se, msg := waitTerminated(ctx, p.stream); se != nil {
				return &inputCompletion{err: se, msg: msg}
			}
			return &inputCompletion{err: &preview2.StreamError{LastOpFailed: true}}
		}
	}
}

func (p *splicePending) run(ctx context.Context) any {
	for {
		if se, msg := waitTerminated(ctx, p.src, p.dst); se != nil {
			return &spliceCompletion{n: p.transferred, err: se, msg: msg}
		}
		n, se, blocked := spliceOnce(p.src, p.dst, p.remaining)
		if !blocked {
			p.transferred += n
			if p.remaining >= n {
				p.remaining -= n
			} else {
				p.remaining = 0
			}
			return &spliceCompletion{n: p.transferred, err: se}
		}
		source := spliceWaitSource(p.src, p.dst)
		if source == nil {
			return &spliceCompletion{n: p.transferred, err: &preview2.StreamError{LastOpFailed: true}}
		}
		if err := waitPollable(ctx, source); err != nil {
			if se, msg := waitTerminated(ctx, p.src, p.dst); se != nil {
				return &spliceCompletion{n: p.transferred, err: se, msg: msg}
			}
			return &spliceCompletion{n: p.transferred, err: &preview2.StreamError{LastOpFailed: true}}
		}
	}
}

func spliceWaitSource(src, dst any) preview2.Pollable {
	if checker, ok := dst.(interface{ CheckWrite() (uint64, error) }); ok {
		permit, err := checker.CheckWrite()
		if err == nil && permit == 0 {
			if subscriber, ok := dst.(interface{ Subscribe() preview2.Pollable }); ok {
				return subscriber.Subscribe()
			}
			return nil
		}
	}
	if subscriber, ok := src.(interface{ Subscribe() preview2.Pollable }); ok {
		return subscriber.Subscribe()
	}
	// Output readiness cannot wake an input that has no notification source.
	return nil
}

func spliceOnce(src, dst any, length uint64) (uint64, *preview2.StreamError, bool) {
	srcStream, ok := src.(interface{ Read(uint64) ([]byte, error) })
	if !ok {
		return 0, &preview2.StreamError{Closed: true}, false
	}
	dstStream, ok := dst.(interface{ Write([]byte) error })
	if !ok {
		return 0, &preview2.StreamError{Closed: true}, false
	}
	if length > preview2.MaxAllocationSize {
		return 0, &preview2.StreamError{LastOpFailed: true}, false
	}
	if checker, ok := dst.(interface{ CheckWrite() (uint64, error) }); ok {
		permit, err := checker.CheckWrite()
		if err != nil {
			return 0, streamFailure(err), false
		}
		if permit == 0 {
			if length == 0 {
				return 0, nil, false
			}
			return 0, nil, true
		}
		length = min(length, permit)
	}
	if length == 0 {
		return 0, nil, false
	}
	if !streamReady(src) {
		return 0, nil, true
	}
	data, err := srcStream.Read(length)
	if err != nil {
		return 0, streamFailure(err), false
	}
	if len(data) == 0 {
		return 0, nil, true
	}
	if err := dstStream.Write(data); err != nil {
		return 0, streamFailure(err), false
	}
	return uint64(len(data)), nil, false
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
		return h.ownedStreamError(result.err, result.msg), true
	}
	resource, ok := h.resources.Get(self)
	if !ok {
		return &preview2.StreamError{Closed: true}, true
	}
	stream, ok := resource.(bufferedOutput)
	if !ok {
		return nil, false
	}
	task := &outputPending{stream: stream, remaining: contents, deadline: socketStreamDeadline(ctx, stream)}
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

func (h *StreamsHost) blockingInput(ctx context.Context, self uint32, length uint64, skip bool) ([]byte, uint64, *preview2.StreamError, bool) {
	async := wasmengine.GetAsyncify(ctx)
	if async != nil && async.IsRewinding(ctx) {
		token, err := wasmengine.Resume(ctx)
		value, ok := wippyhost.GetAsyncValueStore(ctx).Take(token)
		if err != nil {
			panic(fmt.Errorf("stream resume: %w", err))
		}
		result, typed := value.(*inputCompletion)
		if !ok || !typed || result == nil {
			panic("stream completion missing")
		}
		return result.data, result.n, h.ownedStreamError(result.err, result.msg), true
	}
	if length == 0 || length > preview2.MaxAllocationSize {
		return nil, 0, nil, false
	}
	resource, ok := h.resources.Get(self)
	if !ok {
		return nil, 0, nil, false
	}
	stream, ok := resource.(bufferedInput)
	if !ok {
		return nil, 0, nil, false
	}
	deadline := socketStreamDeadline(ctx, stream)
	if data, n, err, done := readReadyInput(stream, length, skip); done {
		return data, n, h.streamError(err), true
	}
	if async == nil {
		panic("blocking stream requires asyncify scheduler context")
	}
	task := &inputPending{stream: stream, length: length, skip: skip, deadline: deadline}
	if err := wasmengine.Suspend(ctx, task); err != nil {
		panic(fmt.Errorf("stream suspend: %w", err))
	}
	return nil, 0, nil, true
}

func (h *StreamsHost) blockingSplice(ctx context.Context, self, src uint32, length uint64) (uint64, *preview2.StreamError, bool) {
	async := wasmengine.GetAsyncify(ctx)
	if async != nil && async.IsRewinding(ctx) {
		token, err := wasmengine.Resume(ctx)
		value, ok := wippyhost.GetAsyncValueStore(ctx).Take(token)
		if err != nil {
			panic(fmt.Errorf("stream resume: %w", err))
		}
		result, typed := value.(*spliceCompletion)
		if !ok || !typed || result == nil {
			panic("stream completion missing")
		}
		return result.n, h.ownedStreamError(result.err, result.msg), true
	}
	srcR, ok := h.resources.Get(src)
	if !ok {
		return 0, &preview2.StreamError{Closed: true}, true
	}
	dstR, ok := h.resources.Get(self)
	if !ok {
		return 0, &preview2.StreamError{Closed: true}, true
	}
	if _, ok := srcR.(interface{ Read(uint64) ([]byte, error) }); !ok {
		return 0, &preview2.StreamError{Closed: true}, true
	}
	if _, ok := dstR.(interface{ Write([]byte) error }); !ok {
		return 0, &preview2.StreamError{Closed: true}, true
	}
	if length > preview2.MaxAllocationSize {
		return 0, h.operationFailed(), true
	}
	deadline := socketStreamDeadline(ctx, srcR, dstR)
	n, se, blocked := spliceOnce(srcR, dstR, length)
	if !blocked {
		return n, h.streamError(se), true
	}
	_, srcWait := srcR.(interface{ Subscribe() preview2.Pollable })
	_, dstWait := dstR.(interface{ Subscribe() preview2.Pollable })
	if !srcWait && !dstWait {
		return n, h.streamError(se), true
	}
	if async == nil {
		panic("blocking splice requires asyncify scheduler context")
	}
	task := &splicePending{
		src:       srcR,
		dst:       dstR,
		remaining: length,
		deadline:  deadline,
	}
	if err := wasmengine.Suspend(ctx, task); err != nil {
		panic(fmt.Errorf("stream suspend: %w", err))
	}
	return 0, nil, true
}

// Materialize owned WASI error handles on the guest worker, never in a dispatcher
// goroutine where resource-limit traps could escape the host call boundary.
func (h *StreamsHost) streamError(err *preview2.StreamError) *preview2.StreamError {
	return h.ownedStreamError(err, "")
}

func (h *StreamsHost) ownedStreamError(err *preview2.StreamError, message string) *preview2.StreamError {
	if err == nil || !err.LastOpFailed || err.LastOpFailedErr != 0 {
		return err
	}
	if message == "" {
		message = err.Error()
	}
	handle := h.resources.Add(preview2.NewErrorResource(message))
	return &preview2.StreamError{LastOpFailed: true, LastOpFailedErr: handle}
}

func (h *StreamsHost) operationFailed() *preview2.StreamError {
	return h.streamError(&preview2.StreamError{LastOpFailed: true})
}
