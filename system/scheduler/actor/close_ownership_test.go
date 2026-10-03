// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"errors"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	apiruntime "github.com/wippyai/runtime/api/runtime"
)

// closeCountingProcess counts Close calls.
type closeCountingProcess struct {
	upgradeSource
	closed atomic.Int64
}

func (p *closeCountingProcess) Close() { p.closed.Add(1) }

// A process whose admission fails stays owned by the submitter, which closes it.
func TestSubmitRejectedByLifecycleDoesNotCloseProcess(t *testing.T) {
	sched := newPreemptTestScheduler(1, &testLifecycle{})
	sched.lifecycle = &rejectingLifecycle2{}
	sched.Start()
	defer testStopScheduler(sched)

	p := &closeCountingProcess{}
	if _, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "r"}, p, "", nil); err == nil {
		t.Fatal("expected rejection")
	}
	if got := p.closed.Load(); got != 0 {
		t.Fatalf("scheduler closed a process it did not admit (%d)", got)
	}
}

// Every failed upgrade closes the process exactly once.
func TestFailedUpgradeClosesProcessOnce(t *testing.T) {
	cases := map[string]func(*closeCountingProcess) (process.Factory, *upgradeFailure){
		"create": func(*closeCountingProcess) (process.Factory, *upgradeFailure) {
			return &mockFactory{createFunc: func(registry.ID) (process.Process, *process.Meta, error) {
				return nil, nil, errors.New("create failed")
			}}, nil
		},
		"init": func(*closeCountingProcess) (process.Factory, *upgradeFailure) {
			target := &upgradeFailure{}
			return &mockFactory{createFunc: func(registry.ID) (process.Process, *process.Meta, error) {
				return target, &process.Meta{Method: "main"}, nil
			}}, target
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			done := make(chan struct{})
			lc := &testLifecycle{onComplete: func(context.Context, pidapi.PID, *apiruntime.Result) { close(done) }}
			sched := newPreemptTestScheduler(1, lc)
			sched.Start()
			defer testStopScheduler(sched)

			source := &closeCountingProcess{upgradeSource: upgradeSource{UpgradeProcess: UpgradeProcess{
				upgradeReq: &process.UpgradeRequest{Source: registry.NewID("app", "next")}}}}
			factory, target := setup(source)
			appCtx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
			process.WithFactory(appCtx, factory)
			frameCtx, fc := ctxapi.OpenFrameContext(appCtx)
			fc.Seal()
			if _, err := sched.Submit(frameCtx, pidapi.PID{UniqID: "u"}, source, "", nil); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("actor did not complete")
			}
			time.Sleep(50 * time.Millisecond)
			closes := source.closed.Load()
			if target != nil {
				closes += target.closed.Load()
			}
			wantTotal := int64(1)
			if target != nil {
				wantTotal = 2
			}
			if closes != wantTotal {
				t.Fatalf("expected %d closes across incarnations, got %d", wantTotal, closes)
			}
			if target != nil && target.closed.Load() != 1 {
				t.Fatalf("replacement closed %d times", target.closed.Load())
			}
		})
	}
}

// upgradeFailure is a replacement whose Init fails.
type upgradeFailure struct {
	upgradeTarget
	closed atomic.Int64
}

func (p *upgradeFailure) Init(context.Context, string, payload.Payloads) error {
	return errors.New("init failed")
}
func (p *upgradeFailure) Close() { p.closed.Add(1) }

// waitingProcess yields once and then waits for the completion.
type waitingProcess struct {
	steps atomic.Int64
}

func (*waitingProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *waitingProcess) Step(events []process.Event, out *process.StepOutput) error {
	if p.steps.Add(1) == 1 {
		out.Yield(YieldCmd{}, 7)
		out.WaitForYields()
		return nil
	}
	if len(events) > 0 {
		out.Done(nil)
		return nil
	}
	out.WaitForYields()
	return nil
}
func (*waitingProcess) Send(*relay.Package) error { return nil }
func (*waitingProcess) Close()                    {}

// A completion that arrives after its actor ended is dropped, even when the
// processor slot has been reused by another actor.
func TestStaleYieldCompletionDoesNotWakeReusedProcessor(t *testing.T) {
	received := make(chan dispatcher.ResultReceiver, 1)
	reg := scheduler.NewRegistry()
	reg.Register(CmdYield, dispatcher.HandlerFunc(func(_ context.Context, _ dispatcher.Command, _ uint64, r dispatcher.ResultReceiver) error {
		select {
		case received <- r:
		default:
		}
		return nil
	}))
	reg.Register(CmdComplete, CompleteHandler())
	done := make(chan struct{}, 64)
	lc := &testLifecycle{onComplete: func(context.Context, pidapi.PID, *apiruntime.Result) { done <- struct{}{} }}
	sched := NewScheduler(reg, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer testStopScheduler(sched)

	ctxA, cancelA := context.WithCancel(context.Background())
	a := &waitingProcess{}
	procA, err := sched.Submit(ctxA, pidapi.PID{UniqID: "a"}, a, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var stale dispatcher.ResultReceiver
	select {
	case stale = <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("yield was not dispatched")
	}
	cancelA()
	<-done

	for i := 0; i < 1000; i++ {
		ctxB, cancelB := context.WithCancel(context.Background())
		b := &waitingProcess{}
		procB, err := sched.Submit(ctxB, pidapi.PID{UniqID: "b"}, b, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		for b.steps.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		select {
		case <-received:
		case <-time.After(5 * time.Second):
			t.Fatal("yield was not dispatched")
		}
		if procB == procA {
			stale.CompleteYield(7, nil, nil)
			time.Sleep(50 * time.Millisecond)
			if got := b.steps.Load(); got != 1 {
				t.Fatalf("stale completion woke the reused processor: %d steps", got)
			}
			cancelB()
			<-done
			return
		}
		cancelB()
		<-done
	}
	t.Fatal("processor slot was never reused")
}
