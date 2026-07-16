// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

type resizeGateProcess struct {
	entered chan struct{}
	release chan struct{}
	steps   atomic.Int32
}

func (p *resizeGateProcess) Init(context.Context, string, payload.Payloads) error { return nil }

func (p *resizeGateProcess) Step(_ []process.Event, out *process.StepOutput) error {
	if p.steps.Add(1) == 1 {
		close(p.entered)
		<-p.release
		out.Continue()
		return nil
	}
	out.Done(nil)
	return nil
}

func (*resizeGateProcess) Send(*relay.Package) error { return nil }
func (*resizeGateProcess) Close()                    {}

type resizeImmediateProcess struct{}

func (*resizeImmediateProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (*resizeImmediateProcess) Step(_ []process.Event, out *process.StepOutput) error {
	out.Done(nil)
	return nil
}
func (*resizeImmediateProcess) Send(*relay.Package) error { return nil }
func (*resizeImmediateProcess) Close()                    {}

type resizeFromStepProcess struct {
	scheduler *Scheduler
	result    chan error
}

func (*resizeFromStepProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *resizeFromStepProcess) Step(_ []process.Event, out *process.StepOutput) error {
	p.result <- p.scheduler.ResizeWorkers(1)
	out.Done(nil)
	return nil
}
func (*resizeFromStepProcess) Send(*relay.Package) error { return nil }
func (*resizeFromStepProcess) Close()                    {}

func releaseResizeGate(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func TestSchedulerResizeBeforeStartAndNoOp(t *testing.T) {
	sched := newTestScheduler(1)
	global := sched.global
	original := sched.workerSnapshot()[0]

	if err := sched.ResizeWorkers(4); err != nil {
		t.Fatalf("grow before start: %v", err)
	}
	if got := sched.workerSnapshot(); len(got) != 4 || got[0] != original {
		t.Fatalf("grow replaced existing workers: len=%d", len(got))
	}
	if err := sched.ResizeWorkers(2); err != nil {
		t.Fatalf("shrink before start: %v", err)
	}
	before := sched.workerSnapshot()
	if err := sched.ResizeWorkers(2); err != nil {
		t.Fatalf("no-op resize: %v", err)
	}
	after := sched.workerSnapshot()
	if len(after) != 2 || after[0] != before[0] || after[1] != before[1] {
		t.Fatal("no-op resize changed worker identity")
	}
	if sched.global != global {
		t.Fatal("resize replaced the global queue")
	}

	sched.Start()
	defer testStopScheduler(sched)
	if _, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "prestart-resize"}, &resizeImmediateProcess{}, "", nil); err != nil {
		t.Fatalf("submit after pre-start resize: %v", err)
	}
	waitFor(t, func() bool { return sched.processorCount.Load() == 0 })
}

func TestSchedulerResizeRejectsInvalidAndStopped(t *testing.T) {
	sched := newTestScheduler(1)
	if err := sched.ResizeWorkers(0); !errors.Is(err, ErrInvalidWorkerCount) {
		t.Fatalf("zero workers: got %v", err)
	}
	if err := sched.ResizeWorkers(-1); !errors.Is(err, ErrInvalidWorkerCount) {
		t.Fatalf("negative workers: got %v", err)
	}
	sched.Start()
	testStopScheduler(sched)
	if err := sched.ResizeWorkers(2); !errors.Is(err, process.ErrSchedulerStopping) {
		t.Fatalf("resize after stop: got %v", err)
	}
}

func TestSchedulerResizeRejectsWhileStopping(t *testing.T) {
	sched := newTestScheduler(1)
	gate := &resizeGateProcess{entered: make(chan struct{}), release: make(chan struct{})}
	defer releaseResizeGate(gate.release)
	if _, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "stop-gate"}, gate, "", nil); err != nil {
		t.Fatal(err)
	}
	sched.Start()
	select {
	case <-gate.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not enter blocking step")
	}

	stopDone := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		sched.Stop(ctx)
		close(stopDone)
	}()
	waitFor(t, func() bool { return sched.stopping.Load() })

	started := time.Now()
	if err := sched.ResizeWorkers(2); !errors.Is(err, process.ErrSchedulerStopping) {
		t.Fatalf("resize while stopping: got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("resize waited behind Stop instead of rejecting: %v", elapsed)
	}
	releaseResizeGate(gate.release)
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not complete")
	}
}

func TestSchedulerDoesNotRouteToRetiredWorker(t *testing.T) {
	sched := newTestScheduler(2)
	retired := sched.workerSnapshot()[1]
	if err := sched.ResizeWorkers(1); err != nil {
		t.Fatal(err)
	}

	proc := acquireProcessor()
	defer releaseProcessor(proc)
	proc.lastWorker.Store(1)
	sched.injectOrGlobal(proc)
	if got := sched.global.Pop(); got != proc {
		t.Fatal("processor affinity to retired worker did not fall back to global queue")
	}
	if got := retired.inject.Pop(); got != nil {
		t.Fatal("processor was routed into retired worker inject queue")
	}
	if got := proc.lastWorker.Load(); got != noWorkerAffinity {
		t.Fatalf("retired affinity was not cleared: %d", got)
	}
}

func TestSchedulerGrowReusesRetiredWorkerIDs(t *testing.T) {
	sched := newTestScheduler(3)
	retired := append([]*Worker(nil), sched.workerSnapshot()[1:]...)
	if err := sched.ResizeWorkers(1); err != nil {
		t.Fatal(err)
	}
	if err := sched.ResizeWorkers(3); err != nil {
		t.Fatal(err)
	}
	workers := sched.workerSnapshot()
	for id := 1; id < 3; id++ {
		if workers[id].id != id {
			t.Fatalf("worker %d has ID %d", id, workers[id].id)
		}
		if workers[id] == retired[id-1] {
			t.Fatalf("worker ID %d reused retired worker instance", id)
		}
	}
}

func TestSchedulerShrinkFromRetiringWorkerDoesNotDeadlock(t *testing.T) {
	sched := newTestScheduler(2)
	retiring := sched.workerSnapshot()[1]
	proc := &resizeFromStepProcess{scheduler: sched, result: make(chan error, 1)}
	submitted, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "self-shrink"}, proc, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if popped := sched.global.Pop(); popped != submitted {
		t.Fatal("submitted processor was not in global queue")
	}
	sched.workerSnapshot()[1].inject.Push(submitted)
	sched.Start()
	defer testStopScheduler(sched)

	select {
	case err := <-proc.result:
		if err != nil {
			t.Fatalf("worker-triggered shrink: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker deadlocked while retiring itself")
	}
	if got := len(sched.workerSnapshot()); got != 1 {
		t.Fatalf("active workers: got %d", got)
	}
	select {
	case <-retiring.done:
	case <-time.After(time.Second):
		t.Fatal("self-retiring worker did not exit")
	}
	waitFor(t, func() bool { return sched.retiredExecuted.Load() >= 1 })
}

func TestSchedulerImmediateRegrowRoutesToPublishedWorker(t *testing.T) {
	completed := make(chan string, 3)
	lifecycle := &testLifecycle{onComplete: func(_ context.Context, pid pidapi.PID, _ *runtime.Result) {
		completed <- pid.UniqID
	}}
	sched := newTestSchedulerWithLifecycle(2, lifecycle)
	workers := sched.workerSnapshot()
	oldWorkerOne := workers[1]

	gateZero := &resizeGateProcess{entered: make(chan struct{}), release: make(chan struct{})}
	gateOne := &resizeGateProcess{entered: make(chan struct{}), release: make(chan struct{})}
	for id, item := range []struct {
		gate *resizeGateProcess
		pid  string
	}{{gateZero, "regrow-gate-0"}, {gateOne, "regrow-gate-1"}} {
		proc, err := sched.Submit(context.Background(), pidapi.PID{UniqID: item.pid}, item.gate, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if popped := sched.global.Pop(); popped != proc {
			t.Fatal("gate processor was not in global queue")
		}
		workers[id].inject.Push(proc)
	}
	sched.Start()
	defer testStopScheduler(sched)
	defer releaseResizeGate(gateZero.release)
	defer releaseResizeGate(gateOne.release)
	for id, entered := range []chan struct{}{gateZero.entered, gateOne.entered} {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatalf("worker %d did not enter gate", id)
		}
	}

	if err := sched.ResizeWorkers(1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-oldWorkerOne.done:
		t.Fatal("blocked retiring worker exited before its current step finished")
	default:
	}

	queued, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "regrow-routed"}, &resizeImmediateProcess{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if popped := sched.global.Pop(); popped != queued {
		t.Fatal("queued processor was not in global queue")
	}
	queued.lastWorker.Store(1)
	if err := sched.ResizeWorkers(2); err != nil {
		t.Fatal(err)
	}
	newWorkerOne := sched.workerSnapshot()[1]
	if newWorkerOne.id != oldWorkerOne.id || newWorkerOne == oldWorkerOne {
		t.Fatal("regrow did not create a distinct worker with the reusable ID")
	}
	sched.injectOrGlobal(queued)
	select {
	case id := <-completed:
		if id != "regrow-routed" {
			t.Fatalf("unexpected completion before gates released: %s", id)
		}
	case <-time.After(time.Second):
		t.Fatal("affine work did not route through the newly published worker")
	}
	select {
	case <-oldWorkerOne.done:
		t.Fatal("old same-ID worker unexpectedly left its blocked step")
	default:
	}

	releaseResizeGate(gateOne.release)
	select {
	case <-oldWorkerOne.done:
	case <-time.After(time.Second):
		t.Fatal("old same-ID worker did not retire after its step finished")
	}
	releaseResizeGate(gateZero.release)
}

func TestSchedulerGrowRunsQueuedWorkWithoutReplacingState(t *testing.T) {
	var quickCompleted atomic.Int32
	lifecycle := &testLifecycle{onComplete: func(_ context.Context, pid pidapi.PID, _ *runtime.Result) {
		if pid.UniqID != "grow-blocker" {
			quickCompleted.Add(1)
		}
	}}
	sched := newTestSchedulerWithLifecycle(1, lifecycle)
	global := sched.global
	firstWorker := sched.workerSnapshot()[0]
	sched.Start()
	defer testStopScheduler(sched)

	gate := &resizeGateProcess{entered: make(chan struct{}), release: make(chan struct{})}
	defer releaseResizeGate(gate.release)
	if _, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "grow-blocker"}, gate, "", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gate.entered:
	case <-time.After(time.Second):
		t.Fatal("initial worker did not enter blocking step")
	}

	const quick = 24
	for i := 0; i < quick; i++ {
		id := pidapi.PID{UniqID: fmt.Sprintf("grow-quick-%d", i)}
		if _, err := sched.Submit(context.Background(), id, &resizeImmediateProcess{}, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := sched.ResizeWorkers(3); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return quickCompleted.Load() == quick })
	if sched.global != global || sched.workerSnapshot()[0] != firstWorker {
		t.Fatal("grow replaced scheduler state or existing worker")
	}
	releaseResizeGate(gate.release)
}

func TestSchedulerShrinkFinishesCurrentStepAndHandsOffContinuation(t *testing.T) {
	completed := make(chan string, 2)
	lifecycle := &testLifecycle{onComplete: func(_ context.Context, pid pidapi.PID, _ *runtime.Result) {
		if pid.UniqID != "shrink-survivor-gate" {
			completed <- pid.UniqID
		}
	}}
	sched := newTestSchedulerWithLifecycle(2, lifecycle)
	workers := sched.workerSnapshot()
	survivorGate := &resizeGateProcess{entered: make(chan struct{}), release: make(chan struct{})}
	gate := &resizeGateProcess{entered: make(chan struct{}), release: make(chan struct{})}
	var proc *Processor
	for id, item := range []struct {
		gate *resizeGateProcess
		pid  string
	}{{survivorGate, "shrink-survivor-gate"}, {gate, "shrink-gate"}} {
		submitted, err := sched.Submit(context.Background(), pidapi.PID{UniqID: item.pid}, item.gate, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if popped := sched.global.Pop(); popped != submitted {
			t.Fatal("gate processor was not in global queue")
		}
		workers[id].inject.Push(submitted)
		if id == 1 {
			proc = submitted
		}
	}
	retiring := workers[1]
	sched.Start()
	defer testStopScheduler(sched)
	defer releaseResizeGate(gate.release)
	defer releaseResizeGate(survivorGate.release)

	for id, entered := range []chan struct{}{survivorGate.entered, gate.entered} {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatalf("worker %d did not enter gate", id)
		}
	}
	queued, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "shrink-injected"}, &resizeImmediateProcess{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if popped := sched.global.Pop(); popped != queued {
		t.Fatal("injected processor was not in global queue")
	}
	queued.lastWorker.Store(1)
	if !sched.workerSnapshot()[1].injectProcessor(queued) {
		t.Fatal("could not queue processor on active worker")
	}

	started := time.Now()
	if err := sched.ResizeWorkers(1); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("shrink waited for retiring worker: %v", elapsed)
	}
	if stored, ok := sched.byPID.Load(proc.pid.String()); !ok || stored != proc {
		t.Fatal("resize removed or replaced the live processor")
	}
	if stored, ok := sched.byPID.Load(queued.pid.String()); !ok || stored != queued {
		t.Fatal("resize removed or replaced the injected processor")
	}
	releaseResizeGate(gate.release)
	select {
	case <-retiring.done:
	case <-time.After(time.Second):
		t.Fatal("retiring worker did not finish its step and hand off queues")
	}
	releaseResizeGate(survivorGate.release)
	if got := len(sched.workerSnapshot()); got != 1 {
		t.Fatalf("active workers: got %d", got)
	}
	wantCompleted := map[string]bool{"shrink-gate": false, "shrink-injected": false}
	for range wantCompleted {
		select {
		case id := <-completed:
			if _, ok := wantCompleted[id]; !ok {
				t.Fatalf("unexpected process completed: %s", id)
			}
			wantCompleted[id] = true
		case <-time.After(time.Second):
			t.Fatal("retired worker queues were not handed to the remaining worker")
		}
	}
	for id, done := range wantCompleted {
		if !done {
			t.Fatalf("process did not complete after handoff: %s", id)
		}
	}
	if gate.steps.Load() != 2 {
		t.Fatalf("process steps: got %d, want 2", gate.steps.Load())
	}
}

func TestSchedulerConcurrentResizeAndSubmit(t *testing.T) {
	var completed atomic.Int32
	lifecycle := &testLifecycle{onComplete: func(context.Context, pidapi.PID, *runtime.Result) {
		completed.Add(1)
	}}
	sched := newTestSchedulerWithLifecycle(3, lifecycle)
	sched.Start()
	defer testStopScheduler(sched)

	const submissions = 200
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				target := 1 + ((i + offset) % 8)
				if err := sched.ResizeWorkers(target); err != nil {
					t.Errorf("resize to %d: %v", target, err)
					return
				}
				_ = sched.Stats()
				_ = sched.WorkerStats()
			}
		}(r)
	}
	for i := 0; i < submissions; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := pidapi.PID{UniqID: fmt.Sprintf("concurrent-%d", i)}
			if _, err := sched.Submit(context.Background(), id, &CounterProcess{}, "", testInput(3)); err != nil {
				t.Errorf("submit %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if err := sched.ResizeWorkers(4); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return completed.Load() == submissions })
	if sched.processorCount.Load() != 0 {
		t.Fatalf("processors remained after resize stress: %d", sched.processorCount.Load())
	}
}
