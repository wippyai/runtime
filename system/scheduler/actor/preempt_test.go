// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler"
)

// spinProcess never finishes on its own: every step uses up its budget.
type spinProcess struct {
	enabled atomic.Bool
	release atomic.Bool
	steps   atomic.Int64
}

func (*spinProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *spinProcess) EnablePreemption()                                  { p.enabled.Store(true) }
func (p *spinProcess) Step(_ []process.Event, out *process.StepOutput) error {
	p.steps.Add(1)
	if p.release.Load() {
		out.Done(nil)
		return nil
	}
	out.Preempt()
	return nil
}
func (*spinProcess) Send(*relay.Package) error { return nil }
func (*spinProcess) Close()                    {}

// pingProcess yields rounds times and completes.
type pingProcess struct {
	rounds int
	done   int
}

func (*pingProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *pingProcess) Step(events []process.Event, out *process.StepOutput) error {
	p.done += len(events)
	if p.done >= p.rounds {
		out.Done(nil)
		return nil
	}
	if len(events) > 0 || p.done == 0 {
		out.Yield(YieldCmd{}, uint64(p.done+1))
	}
	out.WaitForYields()
	return nil
}
func (*pingProcess) Send(*relay.Package) error { return nil }
func (*pingProcess) Close()                    {}

// preemptWithPendingYieldProcess dispatches a long sleep and reports
// preemption in the same step; it must run again without the sleep finishing.
type preemptWithPendingYieldProcess struct {
	steps atomic.Int64
}

func (*preemptWithPendingYieldProcess) Init(context.Context, string, payload.Payloads) error {
	return nil
}
func (p *preemptWithPendingYieldProcess) Step(_ []process.Event, out *process.StepOutput) error {
	if p.steps.Add(1) == 1 {
		out.Yield(SleepCmd{Duration: time.Hour}, 1)
		out.Preempt()
		return nil
	}
	out.Done(nil)
	return nil
}
func (*preemptWithPendingYieldProcess) Send(*relay.Package) error { return nil }
func (*preemptWithPendingYieldProcess) Close()                    {}

func newPreemptTestScheduler(workers int, lc *testLifecycle, opts ...Option) *Scheduler {
	registry := scheduler.NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())
	registry.Register(CmdSleep, SleepHandler())
	return NewScheduler(registry, append([]Option{WithWorkers(workers), WithLifecycle(lc)}, opts...)...)
}

func TestSchedulerEnablesPreemptionForPreemptibleProcesses(t *testing.T) {
	sched := newPreemptTestScheduler(1, &testLifecycle{})
	sched.Start()
	defer testStopScheduler(sched)
	p := &spinProcess{}
	p.release.Store(true)
	if _, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "a"}, p, "", nil); err != nil {
		t.Fatal(err)
	}
	if !p.enabled.Load() {
		t.Fatal("expected preemption to be enabled at submit")
	}
	q := &spinProcess{}
	q.release.Store(true)
	if _, err := sched.CreateProcessor(context.Background(), pidapi.PID{UniqID: "b"}, q); err != nil {
		t.Fatal(err)
	}
	if !q.enabled.Load() {
		t.Fatal("expected preemption to be enabled for created processors")
	}
}

// A preempted process goes behind other ready work: on a single worker a
// process that keeps yielding completes while a spinning process runs.
func TestPreemptedProcessYieldsWorkerToReadyProcesses(t *testing.T) {
	var pingDone atomic.Bool
	lc := &testLifecycle{
		onComplete: func(_ context.Context, pid pidapi.PID, _ *runtime.Result) {
			if pid.UniqID == "ping" {
				pingDone.Store(true)
			}
		},
	}
	sched := newPreemptTestScheduler(1, lc)
	sched.Start()
	defer testStopScheduler(sched)

	spin := &spinProcess{}
	defer spin.release.Store(true)
	if _, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "spin"}, spin, "", nil); err != nil {
		t.Fatal(err)
	}
	const rounds = 50
	if _, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "ping"}, &pingProcess{rounds: rounds}, "", nil); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for !pingDone.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !pingDone.Load() {
		t.Fatalf("ready process starved by preempted process (%d spin steps)", spin.steps.Load())
	}
}

// After a preempted step the worker serves its other ready work before the
// preempted process runs again.
func TestPreemptedProcessIsQueuedBehindLocalReadyWork(t *testing.T) {
	sched := newPreemptTestScheduler(1, &testLifecycle{})
	spin := &spinProcess{}
	spinProc, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "spin"}, spin, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	other := &spinProcess{}
	otherProc, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "other"}, other, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	w := sched.workerSnapshot()[0]
	if got := w.findWork(); got != spinProc {
		t.Fatalf("expected the first submission, got %p", got)
	}
	w.executeOne(spinProc)
	if spin.steps.Load() != 1 {
		t.Fatalf("expected one spin step, got %d", spin.steps.Load())
	}
	if got := w.findWork(); got != otherProc {
		t.Fatalf("expected ready work before the preempted process, got %p (spin %p)", got, spinProc)
	}
	if got := w.findWork(); got != spinProc {
		t.Fatalf("expected the preempted process to be runnable again, got %p", got)
	}
}

func TestPreemptedProcessRunsAgainWithPendingYields(t *testing.T) {
	var done atomic.Bool
	lc := &testLifecycle{
		onComplete: func(context.Context, pidapi.PID, *runtime.Result) { done.Store(true) },
	}
	sched := newPreemptTestScheduler(1, lc)
	sched.Start()
	defer testStopScheduler(sched)

	p := &preemptWithPendingYieldProcess{}
	if _, err := sched.Submit(context.Background(), testPID(), p, "", nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !done.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !done.Load() {
		t.Fatalf("preempted process with a pending yield was not run again (%d steps)", p.steps.Load())
	}
}
