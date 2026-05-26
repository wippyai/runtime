// SPDX-License-Identifier: MPL-2.0

package pool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// --- Test Infrastructure ---

type mockDispatcher struct {
	handlers map[dispatcher.CommandID]dispatcher.Handler
}

func newMockDispatcher() *mockDispatcher {
	return &mockDispatcher{handlers: make(map[dispatcher.CommandID]dispatcher.Handler)}
}

func (d *mockDispatcher) Dispatch(cmd dispatcher.Command) dispatcher.Handler {
	return d.handlers[cmd.CmdID()]
}

func (d *mockDispatcher) Register(id dispatcher.CommandID, h dispatcher.Handler) {
	d.handlers[id] = h
}

type mockCommand struct {
	id dispatcher.CommandID
}

func (c *mockCommand) CmdID() dispatcher.CommandID { return c.id }
func (c *mockCommand) String() string              { return "mock" }
func (c *mockCommand) Type() string                { return "mock" }
func (c *mockCommand) ToCommand() any              { return c }
func (c *mockCommand) Release()                    {}

type mockHandler struct {
	handleFunc func(ctx context.Context, cmd dispatcher.Command, tag uint64, recv dispatcher.ResultReceiver) error
}

func (h *mockHandler) Handle(ctx context.Context, cmd dispatcher.Command, tag uint64, recv dispatcher.ResultReceiver) error {
	if h.handleFunc != nil {
		return h.handleFunc(ctx, cmd, tag, recv)
	}
	recv.CompleteYield(tag, nil, nil)
	return nil
}

type mockProcess struct {
	initFunc  func(ctx context.Context, method string, input payload.Payloads) error
	stepFunc  func(events []process.Event, out *process.StepOutput) error
	abortFunc func()
}

// Abort satisfies the executor's optional aborter interface so the
// cancellation path can drain ephemeral producers.
func (p *mockProcess) Abort() {
	if p.abortFunc != nil {
		p.abortFunc()
	}
}

func (p *mockProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	if p.initFunc != nil {
		return p.initFunc(ctx, method, input)
	}
	return nil
}

func (p *mockProcess) Step(events []process.Event, out *process.StepOutput) error {
	if p.stepFunc != nil {
		return p.stepFunc(events, out)
	}
	out.Done(nil)
	return nil
}

func (p *mockProcess) Close() {}

// --- Executor Construction Tests ---

func TestNewExecutor(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	assert.NotNil(t, e)
	assert.NotNil(t, e.queue)
	assert.NotNil(t, e.wake)
	assert.Equal(t, d, e.dispatcher)
}

func TestExecutor_WithExecutionHooks(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	var startCalled, completeCalled bool
	hooks := ExecutionHooks{
		OnStart:    func(context.Context, process.Process) { startCalled = true },
		OnComplete: func(context.Context, *runtime.Result) { completeCalled = true },
	}

	e.WithExecutionHooks(hooks)

	proc := &mockProcess{}
	e.Run(context.Background(), proc, "main", nil)

	assert.True(t, startCalled)
	assert.True(t, completeCalled)
}

// --- Executor Reset Tests ---

func TestExecutor_Reset(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	e.active.Store(true)
	select {
	case e.wake <- struct{}{}:
	default:
	}

	e.Reset()

	assert.False(t, e.active.Load())
	select {
	case <-e.wake:
		t.Error("wake channel should be drained")
	default:
	}
}

// --- Executor Run Tests ---

func TestExecutor_Run_ImmediateDone(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	proc := &mockProcess{
		stepFunc: func(_ []process.Event, out *process.StepOutput) error {
			out.Done(payload.New("result"))
			return nil
		},
	}

	result := e.Run(context.Background(), proc, "main", nil)

	require.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.NotNil(t, result.Value)
}

func TestExecutor_Run_InitError(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	initErr := errors.New("init failed")
	proc := &mockProcess{
		initFunc: func(context.Context, string, payload.Payloads) error {
			return initErr
		},
	}

	result := e.Run(context.Background(), proc, "main", nil)

	require.NotNil(t, result)
	assert.ErrorIs(t, result.Error, initErr)
}

func TestExecutor_Run_StepError(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	stepErr := errors.New("step failed")
	proc := &mockProcess{
		stepFunc: func(_ []process.Event, _ *process.StepOutput) error {
			return stepErr
		},
	}

	result := e.Run(context.Background(), proc, "main", nil)

	require.NotNil(t, result)
	assert.ErrorIs(t, result.Error, stepErr)
}

func TestExecutor_Run_ContextCancelled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping context cancellation test in short mode")
	}

	d := newMockDispatcher()
	e := NewExecutor(d)

	ctx, cancel := context.WithCancel(context.Background())

	proc := &mockProcess{
		stepFunc: func(_ []process.Event, out *process.StepOutput) error {
			out.Yield(&mockCommand{id: 1}, 1)
			return nil
		},
	}

	d.Register(1, &mockHandler{
		handleFunc: func(_ context.Context, _ dispatcher.Command, _ uint64, _ dispatcher.ResultReceiver) error {
			// Start async handler that never completes
			// Executor should detect context cancellation while waiting
			return nil
		},
	})

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	result := e.Run(ctx, proc, "main", nil)

	require.NotNil(t, result)
	assert.ErrorIs(t, result.Error, context.Canceled)
}

func TestExecutor_Run_WithYield(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	stepCount := 0
	yieldCompleted := false
	proc := &mockProcess{
		stepFunc: func(events []process.Event, out *process.StepOutput) error {
			stepCount++
			if stepCount == 1 {
				out.Yield(&mockCommand{id: 1}, 1)
				return nil
			}
			// Check we got yield completion
			for _, ev := range events {
				if ev.Type == process.EventYieldComplete && ev.Tag == 1 {
					yieldCompleted = true
					out.Done(nil)
					return nil
				}
			}
			out.Yield(&mockCommand{id: 1}, 2) // keep yielding
			return nil
		},
	}

	d.Register(1, &mockHandler{
		handleFunc: func(_ context.Context, _ dispatcher.Command, tag uint64, recv dispatcher.ResultReceiver) error {
			recv.CompleteYield(tag, "data", nil)
			return nil
		},
	})

	result := e.Run(context.Background(), proc, "main", nil)

	require.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.True(t, yieldCompleted)
}

func TestExecutor_Run_UnknownCommand(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	stepCount := 0
	gotError := false
	proc := &mockProcess{
		stepFunc: func(events []process.Event, out *process.StepOutput) error {
			stepCount++
			if stepCount == 1 {
				out.Yield(&mockCommand{id: 999}, 1) // unknown command
				return nil
			}
			// Check we got error
			for _, ev := range events {
				if ev.Type == process.EventYieldComplete && ev.Error != nil {
					gotError = true
					out.Done(nil)
					return nil
				}
			}
			out.Done(nil)
			return nil
		},
	}

	result := e.Run(context.Background(), proc, "main", nil)

	require.NotNil(t, result)
	assert.True(t, gotError)
}

func TestExecutor_Run_HandlerError(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	handlerErr := errors.New("handler failed")
	stepCount := 0
	gotHandlerError := false
	proc := &mockProcess{
		stepFunc: func(events []process.Event, out *process.StepOutput) error {
			stepCount++
			if stepCount == 1 {
				out.Yield(&mockCommand{id: 1}, 1)
				return nil
			}
			// Check we got handler error
			for _, ev := range events {
				if ev.Type == process.EventYieldComplete && ev.Error != nil {
					gotHandlerError = true
					out.Done(nil)
					return nil
				}
			}
			out.Done(nil)
			return nil
		},
	}

	d.Register(1, &mockHandler{
		handleFunc: func(context.Context, dispatcher.Command, uint64, dispatcher.ResultReceiver) error {
			return handlerErr
		},
	})

	result := e.Run(context.Background(), proc, "main", nil)

	require.NotNil(t, result)
	assert.True(t, gotHandlerError)
}

// --- Executor Send Tests ---

func TestExecutor_Send_Active(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	// Simulate active executor
	e.active.Store(true)

	pkg := &relay.Package{}
	err := e.Send(pkg)

	assert.NoError(t, err)
}

func TestExecutor_Send_Inactive(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	// Run and complete to increment generation
	proc := &mockProcess{}
	e.Run(context.Background(), proc, "main", nil)

	// Now inactive with different generation
	pkg := &relay.Package{}
	err := e.Send(pkg)

	assert.ErrorIs(t, err, process.ErrProcessNotFound)
}

func TestExecutor_Send_WakesExecutor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping wake test in short mode")
	}

	d := newMockDispatcher()
	e := NewExecutor(d)

	var wg sync.WaitGroup
	wg.Add(1)

	stepCount := atomic.Int32{}
	gotMessage := false
	proc := &mockProcess{
		stepFunc: func(events []process.Event, out *process.StepOutput) error {
			count := stepCount.Add(1)
			if count == 1 {
				out.Idle()
				return nil
			}
			// Check for message
			for _, ev := range events {
				if ev.Type == process.EventMessage {
					gotMessage = true
					out.Done(nil)
					return nil
				}
			}
			out.Idle()
			return nil
		},
	}

	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		_ = e.Send(&relay.Package{})
	}()

	result := e.Run(context.Background(), proc, "main", nil)

	wg.Wait()
	require.NotNil(t, result)
	assert.True(t, gotMessage)
}

// --- CompleteYield Tests ---

func TestExecutor_CompleteYield(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	e.active.Store(true)

	e.CompleteYield(42, "data", nil)

	// Check wake was signaled
	select {
	case <-e.wake:
		// good
	default:
		t.Error("wake should be signaled")
	}
}

func TestExecutor_CompleteYield_WithError(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	e.active.Store(true)

	testErr := errors.New("test error")
	e.CompleteYield(42, nil, testErr)

	select {
	case <-e.wake:
		// good
	default:
		t.Error("wake should be signaled")
	}
}

// --- Concurrent Tests ---

func TestExecutor_ConcurrentSend(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}

	d := newMockDispatcher()
	e := NewExecutor(d)

	e.active.Store(true)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = e.Send(&relay.Package{})
		}()
	}

	wg.Wait()
}

func TestExecutor_ConcurrentCompleteYield(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}

	d := newMockDispatcher()
	e := NewExecutor(d)

	e.active.Store(true)

	var wg sync.WaitGroup
	for i := uint64(0); i < 10; i++ {
		wg.Add(1)
		go func(tag uint64) {
			defer wg.Done()
			e.CompleteYield(tag, nil, nil)
		}(i)
	}

	wg.Wait()
}

// --- StepYield Tests ---

func TestExecutor_Run_StepYield_HasEvents(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	stepCount := 0
	proc := &mockProcess{
		stepFunc: func(events []process.Event, out *process.StepOutput) error {
			stepCount++
			if stepCount == 1 {
				// First step: yield and return StepYield
				out.Yield(&mockCommand{id: 1}, 1)
				return nil
			}
			// Second step: check for yield completion
			for _, ev := range events {
				if ev.Type == process.EventYieldComplete {
					out.Done(nil)
					return nil
				}
			}
			out.Done(nil)
			return nil
		},
	}

	d.Register(1, &mockHandler{
		handleFunc: func(_ context.Context, _ dispatcher.Command, tag uint64, recv dispatcher.ResultReceiver) error {
			recv.CompleteYield(tag, nil, nil)
			return nil
		},
	})

	result := e.Run(context.Background(), proc, "main", nil)

	require.NotNil(t, result)
	assert.Nil(t, result.Error)
}

func TestExecutor_Run_StepYield_ContextCancelled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping context cancellation test in short mode")
	}

	d := newMockDispatcher()
	e := NewExecutor(d)

	ctx, cancel := context.WithCancel(context.Background())

	var stepCount uint64
	proc := &mockProcess{
		stepFunc: func(_ []process.Event, out *process.StepOutput) error {
			stepCount++
			if stepCount == 1 {
				out.Yield(&mockCommand{id: 1}, 1)
				return nil
			}
			// Keep yielding - never complete
			out.Yield(&mockCommand{id: 1}, stepCount)
			return nil
		},
	}

	d.Register(1, &mockHandler{
		handleFunc: func(_ context.Context, _ dispatcher.Command, _ uint64, _ dispatcher.ResultReceiver) error {
			return nil // Don't complete yield - leave pending for cancellation test
		},
	})

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	result := e.Run(ctx, proc, "main", nil)

	require.NotNil(t, result)
	assert.ErrorIs(t, result.Error, context.Canceled)
}

func TestExecutor_Run_AbortsEphemeralsOnCancel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping context cancellation test in short mode")
	}

	d := newMockDispatcher()
	e := NewExecutor(d)

	ctx, cancel := context.WithCancel(context.Background())

	var aborted atomic.Bool
	proc := &mockProcess{
		stepFunc: func(_ []process.Event, out *process.StepOutput) error {
			// Park on a pending yield so Run blocks in the ctx.Done() select.
			out.Yield(&mockCommand{id: 1}, 1)
			return nil
		},
		abortFunc: func() { aborted.Store(true) },
	}
	d.Register(1, &mockHandler{
		handleFunc: func(_ context.Context, _ dispatcher.Command, _ uint64, _ dispatcher.ResultReceiver) error {
			return nil // leave pending so the executor waits for cancellation
		},
	})

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	result := e.Run(ctx, proc, "main", nil)

	require.NotNil(t, result)
	assert.ErrorIs(t, result.Error, context.Canceled)
	assert.True(t, aborted.Load(), "executor must Abort the process on context cancellation so ephemeral producers drain")
}

// --- StepIdle Tests ---

func TestExecutor_Run_StepIdle_ContextCancelled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping context cancellation test in short mode")
	}

	d := newMockDispatcher()
	e := NewExecutor(d)

	ctx, cancel := context.WithCancel(context.Background())

	proc := &mockProcess{
		stepFunc: func(_ []process.Event, out *process.StepOutput) error {
			out.Idle()
			return nil
		},
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	result := e.Run(ctx, proc, "main", nil)

	require.NotNil(t, result)
	assert.ErrorIs(t, result.Error, context.Canceled)
}

func TestExecutor_Run_StepIdle_HasEvents(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	stepCount := 0
	proc := &mockProcess{
		stepFunc: func(events []process.Event, out *process.StepOutput) error {
			stepCount++
			if stepCount == 1 {
				// Queue an event directly before returning Idle
				e.queue.PushDirect(process.Event{Type: process.EventMessage})
				out.Idle()
				return nil
			}
			// Should receive the message
			for _, ev := range events {
				if ev.Type == process.EventMessage {
					out.Done(nil)
					return nil
				}
			}
			out.Idle()
			return nil
		},
	}

	result := e.Run(context.Background(), proc, "main", nil)

	require.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.Equal(t, 2, stepCount)
}

// --- StepUpgrade Tests ---

func TestExecutor_Run_StepUpgrade(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	stepCount := 0
	proc := &mockProcess{
		stepFunc: func(_ []process.Event, out *process.StepOutput) error {
			stepCount++
			if stepCount == 1 {
				out.Upgrade()
				return nil
			}
			// After upgrade (which does nothing in pool), just complete
			out.Done(nil)
			return nil
		},
	}

	result := e.Run(context.Background(), proc, "main", nil)

	require.NotNil(t, result)
	assert.Nil(t, result.Error)
}

// --- StepContinue Tests ---

func TestExecutor_Run_StepContinue_NoYields(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	stepCount := 0
	proc := &mockProcess{
		stepFunc: func(_ []process.Event, out *process.StepOutput) error {
			stepCount++
			if stepCount < 3 {
				out.Continue()
				return nil
			}
			out.Done(nil)
			return nil
		},
	}

	result := e.Run(context.Background(), proc, "main", nil)

	require.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.Equal(t, 3, stepCount)
}

func TestExecutor_Run_StepContinue_WithYieldsAndHasEvents(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	stepCount := 0
	proc := &mockProcess{
		stepFunc: func(events []process.Event, out *process.StepOutput) error {
			stepCount++
			if stepCount == 1 {
				// Yield and continue
				out.Yield(&mockCommand{id: 1}, 1)
				out.Continue()
				return nil
			}
			// Check for yield completion
			for _, ev := range events {
				if ev.Type == process.EventYieldComplete {
					out.Done(nil)
					return nil
				}
			}
			out.Continue()
			return nil
		},
	}

	d.Register(1, &mockHandler{
		handleFunc: func(_ context.Context, _ dispatcher.Command, tag uint64, recv dispatcher.ResultReceiver) error {
			recv.CompleteYield(tag, nil, nil)
			return nil
		},
	})

	result := e.Run(context.Background(), proc, "main", nil)

	require.NotNil(t, result)
	assert.Nil(t, result.Error)
}

func TestExecutor_Run_StepContinue_ContextCancelled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping context cancellation test in short mode")
	}

	d := newMockDispatcher()
	e := NewExecutor(d)

	ctx, cancel := context.WithCancel(context.Background())

	var stepCount uint64
	proc := &mockProcess{
		stepFunc: func(_ []process.Event, out *process.StepOutput) error {
			stepCount++
			// Yield and continue, but handler never completes
			out.Yield(&mockCommand{id: 1}, stepCount)
			out.Continue()
			return nil
		},
	}

	d.Register(1, &mockHandler{
		handleFunc: func(_ context.Context, _ dispatcher.Command, _ uint64, _ dispatcher.ResultReceiver) error {
			return nil // Don't complete yield - leave pending for cancellation test
		},
	})

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	result := e.Run(ctx, proc, "main", nil)

	require.NotNil(t, result)
	assert.ErrorIs(t, result.Error, context.Canceled)
}

// --- Hooks Tests ---

func TestExecutor_Hooks_OnInitError(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	var startCalled, completeCalled bool
	var completeResult *runtime.Result
	e.WithExecutionHooks(ExecutionHooks{
		OnStart: func(context.Context, process.Process) { startCalled = true },
		OnComplete: func(_ context.Context, r *runtime.Result) {
			completeCalled = true
			completeResult = r
		},
	})

	initErr := errors.New("init failed")
	proc := &mockProcess{
		initFunc: func(context.Context, string, payload.Payloads) error {
			return initErr
		},
	}

	e.Run(context.Background(), proc, "main", nil)

	assert.True(t, startCalled)
	assert.True(t, completeCalled)
	require.NotNil(t, completeResult)
	assert.ErrorIs(t, completeResult.Error, initErr)
}

func TestExecutor_Hooks_OnStepError(t *testing.T) {
	d := newMockDispatcher()
	e := NewExecutor(d)

	var completeCalled bool
	var completeResult *runtime.Result
	e.WithExecutionHooks(ExecutionHooks{
		OnComplete: func(_ context.Context, r *runtime.Result) {
			completeCalled = true
			completeResult = r
		},
	})

	stepErr := errors.New("step failed")
	proc := &mockProcess{
		stepFunc: func(_ []process.Event, _ *process.StepOutput) error {
			return stepErr
		},
	}

	e.Run(context.Background(), proc, "main", nil)

	assert.True(t, completeCalled)
	require.NotNil(t, completeResult)
	assert.ErrorIs(t, completeResult.Error, stepErr)
}

// --- Interface Compliance ---

var _ dispatcher.ResultReceiver = (*Executor)(nil)
var _ relay.Receiver = (*Executor)(nil)
