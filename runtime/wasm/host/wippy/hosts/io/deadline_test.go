// SPDX-License-Identifier: MPL-2.0
package io

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	socketapi "github.com/wippyai/runtime/api/socket"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	pollhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/poll"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type stallInput struct {
	signal    chan struct{}
	readErr   error
	data      []byte
	readCalls []uint64
	aborts    atomic.Int32
	mu        sync.Mutex
	ready     bool
	closed    bool
	closeOnce sync.Once
}

func (s *stallInput) Type() preview2.ResourceType  { return preview2.ResourceInputStream }
func (s *stallInput) Drop()                        {}
func (s *stallInput) Block(context.Context)        {}
func (s *stallInput) Subscribe() preview2.Pollable { return s }
func (s *stallInput) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready || s.closed || s.readErr != nil
}
func (s *stallInput) Notify() <-chan struct{} { return s.signal }
func (s *stallInput) Read(length uint64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readCalls = append(s.readCalls, length)
	if s.closed {
		return nil, &preview2.StreamError{Closed: true}
	}
	if s.readErr != nil {
		return nil, s.readErr
	}
	if !s.ready {
		return []byte{}, nil
	}
	if length == 0 {
		return []byte{}, nil
	}
	out := s.data
	if uint64(len(out)) > length {
		out = out[:length]
	}
	return append([]byte(nil), out...), nil
}
func (s *stallInput) AbortSocket() {
	s.aborts.Add(1)
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.closeOnce.Do(func() { close(s.signal) })
}

type stallOutput struct {
	signal    chan struct{}
	writes    [][]byte
	aborts    atomic.Int32
	mu        sync.Mutex
	permit    uint64
	flushN    int
	closed    bool
	closeOnce sync.Once
}

func (s *stallOutput) Type() preview2.ResourceType  { return preview2.ResourceOutputStream }
func (s *stallOutput) Drop()                        {}
func (s *stallOutput) Block(context.Context)        {}
func (s *stallOutput) Subscribe() preview2.Pollable { return s }
func (s *stallOutput) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.permit > 0 || s.closed
}
func (s *stallOutput) Notify() <-chan struct{} { return s.signal }
func (s *stallOutput) CheckWrite() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, &preview2.StreamError{Closed: true}
	}
	return s.permit, nil
}
func (s *stallOutput) Write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return &preview2.StreamError{Closed: true}
	}
	s.writes = append(s.writes, append([]byte(nil), data...))
	if uint64(len(data)) > s.permit {
		return preview2.ErrWritePermit
	}
	s.permit -= uint64(len(data))
	return nil
}
func (s *stallOutput) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return &preview2.StreamError{Closed: true}
	}
	s.flushN++
	return nil
}
func (s *stallOutput) AbortSocket() {
	s.aborts.Add(1)
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.closeOnce.Do(func() { close(s.signal) })
}

type subscribeInput struct {
	signal chan struct{}
	testInputStream
}

func (s *subscribeInput) Subscribe() preview2.Pollable { return s }
func (s *subscribeInput) Ready() bool                  { return false }
func (s *subscribeInput) Notify() <-chan struct{}      { return s.signal }
func (s *subscribeInput) Block(context.Context)        {}

func deadlineHostContext(t *testing.T, timeoutMS int) (context.Context, *wasmengine.Scheduler, *wasmengine.Asyncify, *wippyhost.AsyncValueStore) {
	t.Helper()
	ctx, scheduler, async, store := bufferedHostContext(t)
	return wippyhost.WithCallLimits(ctx, wasmapi.LimitsConfig{SocketTimeoutMS: timeoutMS}), scheduler, async, store
}

type toCommand interface {
	ToCommand() dispatcher.Command
}

func takeStreamWait(ctx context.Context, t *testing.T, scheduler *wasmengine.Scheduler) (toCommand, *socketapi.StreamWaitCmd) {
	t.Helper()
	step, err := scheduler.Step(ctx, nil)
	require.NoError(t, err)
	pending, ok := step.PendingOp.(toCommand)
	require.True(t, ok)
	cmd, ok := pending.ToCommand().(*socketapi.StreamWaitCmd)
	require.True(t, ok)
	require.NotNil(t, cmd.Run)
	return pending, cmd
}

func resumeResult(ctx context.Context, t *testing.T, scheduler *wasmengine.Scheduler, store *wippyhost.AsyncValueStore, result any) {
	t.Helper()
	_, err := scheduler.Step(ctx, &wasmengine.YieldResult{Value: store.Put(result)})
	require.Error(t, err)
}

func runWithDeadline(t *testing.T, cmd *socketapi.StreamWaitCmd) any {
	t.Helper()
	require.False(t, cmd.Deadline.IsZero())
	waitCtx, cancel := context.WithDeadline(context.Background(), cmd.Deadline)
	defer cancel()
	done := make(chan any, 1)
	go func() { done <- cmd.Run(waitCtx) }()
	select {
	case result := <-done:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("stream wait did not finish")
	}
	return nil
}

func lastOpFailed(t *testing.T, table *preview2.ResourceTable, err *preview2.StreamError) {
	t.Helper()
	require.NotNil(t, err)
	require.True(t, err.LastOpFailed)
	require.False(t, err.Closed)
	require.NotZero(t, err.LastOpFailedErr)
	resource, ok := table.Get(err.LastOpFailedErr)
	require.True(t, ok)
	debug, ok := resource.(*preview2.ErrorResource)
	require.True(t, ok)
	require.Equal(t, streamTimeoutDebug, debug.ToDebugString())
}

func TestBlockingReadTimeoutAbortsTCPStream(t *testing.T) {
	table := preview2.NewResourceTable()
	input := &stallInput{signal: make(chan struct{})}
	handle := table.Add(input)
	host := NewStreamsHost(table)
	ctx, scheduler, async, store := deadlineHostContext(t, 25)
	data, err := host.MethodInputStreamBlockingRead(ctx, handle, 16)
	require.Nil(t, err)
	require.Empty(t, data)
	require.True(t, async.IsUnwinding(ctx))
	pending, cmd := takeStreamWait(ctx, t, scheduler)
	require.Equal(t, cmd.Deadline, pending.ToCommand().(*socketapi.StreamWaitCmd).Deadline)
	result := runWithDeadline(t, cmd)
	got := result.(*inputCompletion)
	require.True(t, got.err.LastOpFailed)
	require.False(t, got.err.Closed)
	require.Equal(t, streamTimeoutDebug, got.msg)
	require.Equal(t, int32(1), input.aborts.Load())
	require.Empty(t, input.readCalls)
	resumeResult(ctx, t, scheduler, store, result)
	data, err = host.MethodInputStreamBlockingRead(ctx, handle, 16)
	require.Empty(t, data)
	lastOpFailed(t, table, err)
	require.True(t, async.IsNormal(ctx))
	data, err = host.MethodInputStreamRead(context.Background(), handle, 16)
	require.Empty(t, data)
	require.NotNil(t, err)
	require.True(t, err.Closed)
	require.False(t, err.LastOpFailed)
	input.AbortSocket()
	require.Equal(t, int32(2), input.aborts.Load())
}

func TestBlockingSkipTimeoutAbortsTCPStream(t *testing.T) {
	table := preview2.NewResourceTable()
	input := &stallInput{signal: make(chan struct{})}
	handle := table.Add(input)
	host := NewStreamsHost(table)
	ctx, scheduler, async, store := deadlineHostContext(t, 25)
	n, err := host.MethodInputStreamBlockingSkip(ctx, handle, 8)
	require.Zero(t, n)
	require.Nil(t, err)
	require.True(t, async.IsUnwinding(ctx))
	_, cmd := takeStreamWait(ctx, t, scheduler)
	result := runWithDeadline(t, cmd)
	resumeResult(ctx, t, scheduler, store, result)
	n, err = host.MethodInputStreamBlockingSkip(ctx, handle, 8)
	require.Zero(t, n)
	lastOpFailed(t, table, err)
	require.Equal(t, int32(1), input.aborts.Load())
	require.Empty(t, input.readCalls)
}

func TestBlockingWriteTimeoutHoldsSuffixWithoutReplay(t *testing.T) {
	table := preview2.NewResourceTable()
	output := &stallOutput{signal: make(chan struct{}), permit: 3}
	handle := table.Add(output)
	host := NewStreamsHost(table)
	ctx, scheduler, async, store := deadlineHostContext(t, 25)
	contents := []byte("abcdef")
	err := host.MethodOutputStreamBlockingWriteAndFlush(ctx, handle, contents)
	require.Nil(t, err)
	require.True(t, async.IsUnwinding(ctx))
	require.Equal(t, [][]byte{[]byte("abc")}, output.writes)
	pending, cmd := takeStreamWait(ctx, t, scheduler)
	firstDeadline := cmd.Deadline
	time.Sleep(5 * time.Millisecond)
	require.Equal(t, firstDeadline, pending.ToCommand().(*socketapi.StreamWaitCmd).Deadline)
	result := runWithDeadline(t, cmd)
	got := result.(*outputCompletion)
	require.True(t, got.err.LastOpFailed)
	require.Equal(t, [][]byte{[]byte("abc")}, output.writes)
	require.Equal(t, int32(1), output.aborts.Load())
	resumeResult(ctx, t, scheduler, store, result)
	err = host.MethodOutputStreamBlockingWriteAndFlush(ctx, handle, contents)
	lastOpFailed(t, table, err)
	require.True(t, async.IsNormal(ctx), "rewind must not repeat the written prefix")
	require.Equal(t, [][]byte{[]byte("abc")}, output.writes)
	err = host.MethodOutputStreamWrite(context.Background(), handle, []byte("z"))
	require.NotNil(t, err)
	require.True(t, err.Closed)
}

func TestBlockingFlushAndZeroesTimeoutAbort(t *testing.T) {
	for _, kind := range []string{"flush", "zeroes"} {
		t.Run(kind, func(t *testing.T) {
			table := preview2.NewResourceTable()
			output := &stallOutput{signal: make(chan struct{})}
			if kind == "zeroes" {
				output.permit = 4
			}
			handle := table.Add(output)
			host := NewStreamsHost(table)
			ctx, scheduler, async, store := deadlineHostContext(t, 25)
			var err *preview2.StreamError
			if kind == "flush" {
				err = host.MethodOutputStreamBlockingFlush(ctx, handle)
			} else {
				err = host.MethodOutputStreamBlockingWriteZeroesAndFlush(ctx, handle, 4)
			}
			require.Nil(t, err)
			require.True(t, async.IsUnwinding(ctx))
			_, cmd := takeStreamWait(ctx, t, scheduler)
			result := runWithDeadline(t, cmd)
			resumeResult(ctx, t, scheduler, store, result)
			if kind == "flush" {
				err = host.MethodOutputStreamBlockingFlush(ctx, handle)
			} else {
				err = host.MethodOutputStreamBlockingWriteZeroesAndFlush(ctx, handle, 4)
			}
			lastOpFailed(t, table, err)
			require.Equal(t, int32(1), output.aborts.Load())
			if kind == "zeroes" {
				require.Equal(t, [][]byte{{0, 0, 0, 0}}, output.writes)
			} else {
				require.Empty(t, output.writes)
				require.Equal(t, 1, output.flushN)
			}
		})
	}
}

func TestBlockingSpliceTimeoutDoesNotConsumeInput(t *testing.T) {
	table := preview2.NewResourceTable()
	input := &stallInput{signal: make(chan struct{}), data: []byte("payload"), ready: true}
	output := &stallOutput{signal: make(chan struct{})}
	src := table.Add(input)
	dst := table.Add(output)
	host := NewStreamsHost(table)
	ctx, scheduler, async, store := deadlineHostContext(t, 25)
	n, err := host.MethodOutputStreamBlockingSplice(ctx, dst, src, 8)
	require.Zero(t, n)
	require.Nil(t, err)
	require.True(t, async.IsUnwinding(ctx))
	require.Empty(t, input.readCalls)
	_, cmd := takeStreamWait(ctx, t, scheduler)
	result := runWithDeadline(t, cmd)
	got := result.(*spliceCompletion)
	require.Zero(t, got.n)
	require.True(t, got.err.LastOpFailed)
	require.Empty(t, input.readCalls)
	require.Empty(t, output.writes)
	resumeResult(ctx, t, scheduler, store, result)
	n, err = host.MethodOutputStreamBlockingSplice(ctx, dst, src, 8)
	require.Zero(t, n)
	lastOpFailed(t, table, err)
	require.Equal(t, int32(1), input.aborts.Load())
	require.Equal(t, int32(1), output.aborts.Load())
	require.Empty(t, input.readCalls)
}

func TestBlockingSpliceWaitsForInputAfterOutputPermit(t *testing.T) {
	table := preview2.NewResourceTable()
	input := &stallInput{signal: make(chan struct{})}
	output := &stallOutput{signal: make(chan struct{}), permit: 16}
	src := table.Add(input)
	dst := table.Add(output)
	host := NewStreamsHost(table)
	ctx, scheduler, async, store := deadlineHostContext(t, 25)
	n, err := host.MethodOutputStreamBlockingSplice(ctx, dst, src, 8)
	require.Zero(t, n)
	require.Nil(t, err)
	require.True(t, async.IsUnwinding(ctx))
	require.Empty(t, input.readCalls)
	_, cmd := takeStreamWait(ctx, t, scheduler)
	result := runWithDeadline(t, cmd)
	resumeResult(ctx, t, scheduler, store, result)
	n, err = host.MethodOutputStreamBlockingSplice(ctx, dst, src, 8)
	require.Zero(t, n)
	lastOpFailed(t, table, err)
	require.Empty(t, output.writes)
}

func TestNonTCPBlockingReadHasNoDeadline(t *testing.T) {
	table := preview2.NewResourceTable()
	input := &subscribeInput{signal: make(chan struct{})}
	handle := table.Add(input)
	host := NewStreamsHost(table)
	ctx, scheduler, async, _ := deadlineHostContext(t, 25)
	data, err := host.MethodInputStreamBlockingRead(ctx, handle, 4)
	require.Nil(t, err)
	require.Empty(t, data)
	require.True(t, async.IsUnwinding(ctx))
	_, cmd := takeStreamWait(ctx, t, scheduler)
	require.True(t, cmd.Deadline.IsZero())
}

func TestGenericPollUnaffectedBySocketTimeout(t *testing.T) {
	host, table, input, _, _ := bufferedPair(t)
	ctx, scheduler, async, _ := deadlineHostContext(t, 20)
	poll := pollhost.NewHost(table)
	sub := host.MethodInputStreamSubscribe(ctx, input)
	require.Nil(t, poll.Poll(ctx, []uint32{sub}))
	require.True(t, async.IsUnwinding(ctx))
	step, err := scheduler.Step(ctx, nil)
	require.NoError(t, err)
	cmd, ok := step.PendingOp.(interface{ ToCommand() dispatcher.Command }).ToCommand().(*socketapi.PollWaitCmd)
	require.True(t, ok)
	waitCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = cmd.Wait(waitCtx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestBlockingOutputSuccessResumeAfterDeadline(t *testing.T) {
	table := preview2.NewResourceTable()
	output := &stallOutput{signal: make(chan struct{}), permit: 16}
	handle := table.Add(output)
	host := NewStreamsHost(table)
	ctx, scheduler, async, store := deadlineHostContext(t, 10)
	token := store.Put(&outputCompletion{})
	_, err := scheduler.Step(ctx, &wasmengine.YieldResult{Value: token})
	require.Error(t, err)
	require.True(t, async.IsRewinding(ctx))
	time.Sleep(20 * time.Millisecond)
	require.Nil(t, host.MethodOutputStreamBlockingWriteAndFlush(ctx, handle, []byte("late")))
	require.True(t, async.IsNormal(ctx))
	require.Empty(t, output.writes)
	require.Equal(t, int32(0), output.aborts.Load())
}

func TestBlockingReadParentCancelDoesNotAbort(t *testing.T) {
	table := preview2.NewResourceTable()
	input := &stallInput{signal: make(chan struct{})}
	handle := table.Add(input)
	host := NewStreamsHost(table)
	ctx, scheduler, async, _ := deadlineHostContext(t, 500)
	data, err := host.MethodInputStreamBlockingRead(ctx, handle, 4)
	require.Nil(t, err)
	require.Empty(t, data)
	require.True(t, async.IsUnwinding(ctx))
	_, cmd := takeStreamWait(ctx, t, scheduler)
	parent, cancel := context.WithCancel(context.Background())
	waitCtx, stop := context.WithDeadline(parent, cmd.Deadline)
	defer stop()
	done := make(chan any, 1)
	go func() { done <- cmd.Run(waitCtx) }()
	cancel()
	select {
	case result := <-done:
		got := result.(*inputCompletion)
		require.True(t, got.err.Closed)
		require.False(t, got.err.LastOpFailed)
		require.Equal(t, int32(0), input.aborts.Load())
	case <-time.After(3 * time.Second):
		t.Fatal("canceled wait did not finish")
	}
}

func TestBlockingReadValidatesBeforeWait(t *testing.T) {
	table := preview2.NewResourceTable()
	input := &stallInput{signal: make(chan struct{})}
	handle := table.Add(input)
	host := NewStreamsHost(table)
	ctx, _, async, _ := deadlineHostContext(t, 25)
	_, err := host.MethodInputStreamBlockingRead(ctx, handle, preview2.MaxAllocationSize+1)
	require.NotNil(t, err)
	require.True(t, err.LastOpFailed)
	require.True(t, async.IsNormal(ctx))
	require.Equal(t, int32(0), input.aborts.Load())
	_, err = host.MethodInputStreamBlockingRead(ctx, handle+9, 4)
	require.NotNil(t, err)
	require.True(t, err.Closed)
	require.True(t, async.IsNormal(ctx))
}

func TestTCPAbortSocketDeadline(t *testing.T) {
	host, table, input, _, _ := bufferedPair(t)
	ctx, scheduler, async, store := deadlineHostContext(t, 25)
	used := table.SocketBudget().Used()
	require.NotZero(t, used)
	data, err := host.MethodInputStreamBlockingRead(ctx, input, 8)
	require.Nil(t, err)
	require.Empty(t, data)
	require.True(t, async.IsUnwinding(ctx))
	_, cmd := takeStreamWait(ctx, t, scheduler)
	require.False(t, cmd.Deadline.IsZero())
	result := runWithDeadline(t, cmd)
	resumeResult(ctx, t, scheduler, store, result)
	data, err = host.MethodInputStreamBlockingRead(ctx, input, 8)
	require.Empty(t, data)
	lastOpFailed(t, table, err)
	require.Equal(t, used, table.SocketBudget().Used())
	data, err = host.MethodInputStreamRead(context.Background(), input, 8)
	require.Empty(t, data)
	require.NotNil(t, err)
	require.True(t, err.Closed)
	require.False(t, err.LastOpFailed)
}

func TestBlockingInputEmptyWakeDoesNotComplete(t *testing.T) {
	for _, skip := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "skip"}[skip], func(t *testing.T) {
			table := preview2.NewResourceTable()
			input := &stallInput{signal: make(chan struct{}), ready: true}
			handle := table.Add(input)
			host := NewStreamsHost(table)
			ctx, scheduler, async, store := deadlineHostContext(t, 1000)
			data, n, err, handled := host.blockingInput(ctx, handle, 4, skip)
			require.True(t, handled)
			require.Nil(t, err)
			require.Empty(t, data)
			require.Zero(t, n)
			require.True(t, async.IsUnwinding(ctx), "empty read completed instead of parking")
			_, cmd := takeStreamWait(ctx, t, scheduler)
			_, _, _, done := readReadyInput(input, 4, skip)
			require.False(t, done, "empty wake completed pending read")
			input.data = []byte("done")
			result := runWithDeadline(t, cmd)
			resumeResult(ctx, t, scheduler, store, result)
			data, n, err, handled = host.blockingInput(ctx, handle, 4, skip)
			require.True(t, handled)
			require.Nil(t, err)
			if skip {
				require.Equal(t, uint64(4), n)
				require.Empty(t, data)
			} else {
				require.Equal(t, "done", string(data))
			}
			require.True(t, async.IsNormal(ctx))
		})
	}
}

func TestSpliceWithoutInputNotificationDoesNotSpin(t *testing.T) {
	input := &testInputStream{}
	output := &stallOutput{signal: make(chan struct{}), permit: 16}
	pending := &splicePending{src: input, dst: output, remaining: 4}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result := pending.run(ctx).(*spliceCompletion)
	require.NotNil(t, result.err)
	require.True(t, result.err.LastOpFailed)
	require.Empty(t, result.msg, "unavailable notification must not spin until timeout")
	require.Equal(t, []uint64{4}, input.readCalls)
	require.Zero(t, output.aborts.Load())
}
