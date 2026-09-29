// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	sysprocess "github.com/wippyai/runtime/system/process"
)

type terminateDuringStepProcess struct {
	ctx     context.Context
	entered chan struct{}
}

// cancelledOutcomeProcess exercises outputs that a cancellation-aware engine
// can produce when its executing step is interrupted.
type cancelledOutcomeProcess struct {
	err error
	terminateDuringStepProcess
	status process.StepStatus
}

func (p *cancelledOutcomeProcess) Step(_ []process.Event, out *process.StepOutput) error {
	close(p.entered)
	<-p.ctx.Done()
	switch p.status {
	case process.StepDone:
		out.Done(payload.NewString("cancelled result"))
	case process.StepContinue:
		out.Continue()
	case process.StepIdle:
		out.Idle()
	case process.StepYield:
		out.Yield(CompleteCmd{}, 1)
		out.WaitForYields()
	case process.StepUpgrade:
		out.SetUpgrade(&process.UpgradeRequest{})
	}
	return p.err
}

func TestTerminateDuringStepOverridesCancelledOutcome(t *testing.T) {
	for _, tc := range []struct {
		err    error
		name   string
		status process.StepStatus
	}{
		{name: "done with value", status: process.StepDone},
		{name: "continue", status: process.StepContinue},
		{name: "idle", status: process.StepIdle},
		{name: "yield", status: process.StepYield},
		{name: "upgrade", status: process.StepUpgrade},
		{name: "context error", status: process.StepDone, err: context.Canceled},
		{name: "normal exit error", status: process.StepDone, err: supervisor.ErrExit},
		{name: "step error", status: process.StepDone, err: errors.New("interrupted step")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			completed := make(chan *runtime.Result, 1)
			sched := newTestSchedulerWithLifecycle(1, &testLifecycle{onComplete: func(_ context.Context, _ pidapi.PID, res *runtime.Result) {
				completed <- res
			}})
			sched.Start()
			defer testStopScheduler(sched)
			p := &cancelledOutcomeProcess{
				terminateDuringStepProcess: terminateDuringStepProcess{entered: make(chan struct{})},
				status:                     tc.status, err: tc.err,
			}
			id := pidapi.PID{UniqID: "cancelled-outcome"}
			_, err := sched.Submit(context.Background(), id, p, "", nil)
			require.NoError(t, err)
			select {
			case <-p.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("process did not enter its step")
			}
			require.NoError(t, sched.Terminate(id))
			select {
			case res := <-completed:
				require.ErrorIs(t, res.Error, sysprocess.ErrTerminated)
				require.Nil(t, res.Value, "cancelled output must not become a return value")
			case <-time.After(5 * time.Second):
				t.Fatal("terminated process did not complete")
			}
		})
	}
}

type completedStepProcess struct {
	statsEntered chan struct{}
	releaseStats chan struct{}
}

func (*completedStepProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (*completedStepProcess) Step(_ []process.Event, out *process.StepOutput) error {
	out.Done(payload.NewString("completed before termination"))
	return nil
}
func (p *completedStepProcess) Stats() attrs.Attributes {
	close(p.statsEntered)
	<-p.releaseStats
	return nil
}
func (*completedStepProcess) Close() {}

func TestTerminationAfterStepOutcomePreservesReturn(t *testing.T) {
	for _, parentCancellation := range []bool{false, true} {
		name := "terminate"
		if parentCancellation {
			name = "parent cancellation"
		}
		t.Run(name, func(t *testing.T) {
			completed := make(chan *runtime.Result, 1)
			sched := newTestSchedulerWithLifecycle(1, &testLifecycle{onComplete: func(_ context.Context, _ pidapi.PID, res *runtime.Result) {
				completed <- res
			}})
			sched.EnableStats()
			sched.Start()
			defer testStopScheduler(sched)
			p := &completedStepProcess{statsEntered: make(chan struct{}), releaseStats: make(chan struct{})}
			var releaseOnce sync.Once
			releaseStats := func() { releaseOnce.Do(func() { close(p.releaseStats) }) }
			defer releaseStats()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			id := pidapi.PID{UniqID: "completed-step"}
			_, err := sched.Submit(ctx, id, p, "", nil)
			require.NoError(t, err)
			select {
			case <-p.statsEntered:
			case <-time.After(5 * time.Second):
				t.Fatal("process did not reach stats after returning from Step")
			}
			if parentCancellation {
				cancel()
			} else {
				require.NoError(t, sched.Terminate(id))
			}
			releaseStats()
			select {
			case res := <-completed:
				require.NoError(t, res.Error)
				require.NotNil(t, res.Value)
				require.Equal(t, "completed before termination", res.Value.Data())
			case <-time.After(5 * time.Second):
				t.Fatal("completed step did not report its result")
			}
		})
	}
}

type completionDiscardGate struct {
	entered chan struct{}
	release chan struct{}
}

func (g *completionDiscardGate) DiscardEvent() {
	close(g.entered)
	<-g.release
}

type completedOutcomeProcess struct {
	entered chan struct{}
	release chan struct{}
	err     error
}

func (*completedOutcomeProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *completedOutcomeProcess) Step(_ []process.Event, out *process.StepOutput) error {
	close(p.entered)
	<-p.release
	out.Done(payload.NewString("completed"))
	return p.err
}
func (*completedOutcomeProcess) Close() {}

func TestTerminationAfterCompletionDecisionPreservesOutcome(t *testing.T) {
	for _, tc := range []struct {
		err  error
		name string
	}{
		{name: "return"},
		{name: "step error", err: errors.New("completed with error")},
		{name: "normal exit", err: supervisor.ErrExit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			completed := make(chan *runtime.Result, 1)
			sched := newTestSchedulerWithLifecycle(1, &testLifecycle{onComplete: func(_ context.Context, _ pidapi.PID, res *runtime.Result) {
				completed <- res
			}})
			sched.Start()
			defer testStopScheduler(sched)
			p := &completedOutcomeProcess{entered: make(chan struct{}), release: make(chan struct{}), err: tc.err}
			var releaseOnce sync.Once
			releaseStep := func() { releaseOnce.Do(func() { close(p.release) }) }
			defer releaseStep()
			gate := &completionDiscardGate{entered: make(chan struct{}), release: make(chan struct{})}
			var gateOnce sync.Once
			releaseGate := func() { gateOnce.Do(func() { close(gate.release) }) }
			defer releaseGate()
			id := pidapi.PID{UniqID: "completion-decision"}
			proc, err := sched.Submit(context.Background(), id, p, "", nil)
			require.NoError(t, err)
			select {
			case <-p.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("process did not enter its step")
			}
			// Queue teardown happens after the worker commits StateComplete,
			// before finishProcessor publishes the result to the lifecycle.
			require.True(t, proc.queue.Push(process.Event{Type: process.EventYieldComplete, Data: gate}, proc.gen.Load()))
			releaseStep()
			select {
			case <-gate.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("completed process did not reach queue teardown")
			}
			require.NoError(t, sched.Terminate(id))
			releaseGate()
			select {
			case res := <-completed:
				require.Equal(t, tc.err, res.Error)
				if tc.err == nil {
					require.Equal(t, "completed", res.Value.Data())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("completed process did not report its outcome")
			}
		})
	}
}

func (p *terminateDuringStepProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	return nil
}

func (p *terminateDuringStepProcess) Step(_ []process.Event, out *process.StepOutput) error {
	close(p.entered)
	<-p.ctx.Done()
	out.Done(nil)
	return nil
}

func (*terminateDuringStepProcess) Send(*relay.Package) error { return nil }
func (*terminateDuringStepProcess) Close()                    {}

func TestTerminateDuringStepReportsTermination(t *testing.T) {
	completed := make(chan *runtime.Result, 1)
	lc := &testLifecycle{onComplete: func(_ context.Context, _ pidapi.PID, res *runtime.Result) {
		completed <- res
	}}
	sched := newTestSchedulerWithLifecycle(1, lc)
	sched.Start()
	defer testStopScheduler(sched)

	proc := &terminateDuringStepProcess{entered: make(chan struct{})}
	id := pidapi.PID{UniqID: "terminate-during-step"}
	_, err := sched.Submit(context.Background(), id, proc, "", nil)
	require.NoError(t, err)
	select {
	case <-proc.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not enter its step")
	}
	require.NoError(t, sched.Terminate(id))
	select {
	case res := <-completed:
		require.ErrorIs(t, res.Error, sysprocess.ErrTerminated)
	case <-time.After(5 * time.Second):
		t.Fatal("terminated process did not complete")
	}
}
