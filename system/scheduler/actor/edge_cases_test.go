// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler"
)

// UpgradeProcess requests process upgrade
type UpgradeProcess struct {
	upgradeReq *process.UpgradeRequest
	upgraded   bool
}

func (p *UpgradeProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *UpgradeProcess) Step(_ []process.Event, out *process.StepOutput) error {
	if p.upgraded {
		out.Done(nil)
		return nil
	}
	p.upgraded = true
	out.SetUpgrade(p.upgradeReq)
	return nil
}

func (p *UpgradeProcess) Send(*relay.Package) error { return nil }
func (p *UpgradeProcess) Close()                    {}

// TestUpgradeNoRequest tests upgrade with nil request
func TestUpgradeNoRequest(t *testing.T) {
	reg := scheduler.NewRegistry()
	reg.Register(CmdComplete, CompleteHandler())
	reg.Register(CmdYield, YieldHandler())
	te := newTestExecutorWithRegistry(1, reg)
	te.Start()
	defer te.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pid := pidapi.PID{UniqID: "upgrade-no-req"}
	result, err := te.Execute(ctx, pid, &UpgradeProcess{upgradeReq: nil}, "", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Error == nil || result.Error.Error() != "upgrade: no request" {
		t.Fatalf("expected 'upgrade: no request' error, got %v", result.Error)
	}
}

// TestUpgradeNoFactory tests upgrade without factory in context
func TestUpgradeNoFactory(t *testing.T) {
	reg := scheduler.NewRegistry()
	reg.Register(CmdComplete, CompleteHandler())
	reg.Register(CmdYield, YieldHandler())
	te := newTestExecutorWithRegistry(1, reg)
	te.Start()
	defer te.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pid := pidapi.PID{UniqID: "upgrade-no-factory"}
	proc := &UpgradeProcess{
		upgradeReq: &process.UpgradeRequest{
			Source: registry.ID{Name: "test"},
		},
	}

	result, err := te.Execute(ctx, pid, proc, "", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Error == nil || result.Error.Error() != "upgrade: no factory" {
		t.Fatalf("expected 'upgrade: no factory' error, got %v", result.Error)
	}
}

// mockFactory implements process.Factory for testing
type mockFactory struct {
	createFunc func(id registry.ID) (process.Process, *process.Meta, error)
}

func (f *mockFactory) Create(id registry.ID) (process.Process, *process.Meta, error) {
	if f.createFunc != nil {
		return f.createFunc(id)
	}
	return nil, nil, errors.New("not implemented")
}

// TestUpgradeNoSource tests upgrade with empty source and no frame ID
func TestUpgradeNoSource(t *testing.T) {
	reg := scheduler.NewRegistry()
	reg.Register(CmdYield, YieldHandler())
	te := newTestExecutorWithRegistry(1, reg)
	te.Start()
	defer te.Stop()

	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	process.WithFactory(ctx, &mockFactory{})
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pid := pidapi.PID{UniqID: "upgrade-no-source"}
	proc := &UpgradeProcess{
		upgradeReq: &process.UpgradeRequest{
			Source: registry.ID{},
		},
	}

	result, err := te.Execute(ctx, pid, proc, "", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Error == nil || result.Error.Error() != "upgrade: no source" {
		t.Fatalf("expected 'upgrade: no source' error, got %v", result.Error)
	}
}

// TestUpgradeCreateFailed tests upgrade when factory.Create fails
func TestUpgradeCreateFailed(t *testing.T) {
	reg := scheduler.NewRegistry()
	te := newTestExecutorWithRegistry(1, reg)
	te.Start()
	defer te.Stop()

	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	process.WithFactory(ctx, &mockFactory{
		createFunc: func(_ registry.ID) (process.Process, *process.Meta, error) {
			return nil, nil, errors.New("create failed")
		},
	})
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pid := pidapi.PID{UniqID: "upgrade-create-fail"}
	proc := &UpgradeProcess{
		upgradeReq: &process.UpgradeRequest{
			Source: registry.ID{Name: "test"},
		},
	}

	result, err := te.Execute(ctx, pid, proc, "", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Error == nil {
		t.Fatal("expected error")
	}
	if result.Error.Error() != "upgrade: create failed: create failed" {
		t.Fatalf("expected 'upgrade: create failed: create failed', got %v", result.Error)
	}
}

// FailingInitProcess fails on Init
type FailingInitProcess struct{}

func (p *FailingInitProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return errors.New("init failed")
}
func (p *FailingInitProcess) Step(_ []process.Event, _ *process.StepOutput) error { return nil }
func (p *FailingInitProcess) Send(*relay.Package) error                           { return nil }
func (p *FailingInitProcess) Close()                                              {}

// TestUpgradeInitFailed tests upgrade when new process Init fails
func TestUpgradeInitFailed(t *testing.T) {
	reg := scheduler.NewRegistry()
	te := newTestExecutorWithRegistry(1, reg)
	te.Start()
	defer te.Stop()

	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	process.WithFactory(ctx, &mockFactory{
		createFunc: func(_ registry.ID) (process.Process, *process.Meta, error) {
			return &FailingInitProcess{}, nil, nil
		},
	})
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pid := pidapi.PID{UniqID: "upgrade-init-fail"}
	proc := &UpgradeProcess{
		upgradeReq: &process.UpgradeRequest{
			Source: registry.ID{Name: "test"},
		},
	}

	result, err := te.Execute(ctx, pid, proc, "", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Error == nil {
		t.Fatal("expected error")
	}
}

// ImmediateProcess completes immediately
type ImmediateProcess struct{}

func (p *ImmediateProcess) Init(_ context.Context, _ string, _ payload.Payloads) error { return nil }
func (p *ImmediateProcess) Step(_ []process.Event, out *process.StepOutput) error {
	out.Done(nil)
	return nil
}
func (p *ImmediateProcess) Send(*relay.Package) error { return nil }
func (p *ImmediateProcess) Close()                    {}

type FrameWritingProcess struct{}

var (
	upgradeInheritedKey = &ctxapi.Key{Name: "upgrade.inherited", Inherit: true}
	upgradeNewKey       = &ctxapi.Key{Name: "upgrade.new"}
)

func (p *FrameWritingProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return errors.New("frame context missing")
	}
	if _, ok := fc.Get(upgradeInheritedKey); !ok {
		return errors.New("inherited frame value missing")
	}
	return fc.Set(upgradeNewKey, "ready")
}
func (p *FrameWritingProcess) Step(_ []process.Event, out *process.StepOutput) error {
	out.Done(nil)
	return nil
}
func (p *FrameWritingProcess) Send(*relay.Package) error { return nil }
func (p *FrameWritingProcess) Close()                    {}

// TestUpgradeSuccess tests successful upgrade
func TestUpgradeSuccess(t *testing.T) {
	reg := scheduler.NewRegistry()
	te := newTestExecutorWithRegistry(1, reg)
	te.Start()
	defer te.Stop()

	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	process.WithFactory(ctx, &mockFactory{
		createFunc: func(_ registry.ID) (process.Process, *process.Meta, error) {
			return &ImmediateProcess{}, &process.Meta{Method: "run"}, nil
		},
	})
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pid := pidapi.PID{UniqID: "upgrade-success"}
	proc := &UpgradeProcess{
		upgradeReq: &process.UpgradeRequest{
			Source: registry.ID{Name: "test"},
		},
	}

	result, err := te.Execute(ctx, pid, proc, "", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
}

func TestUpgradeSuccess_ForksSealedFrameForReplacementProcess(t *testing.T) {
	reg := scheduler.NewRegistry()
	te := newTestExecutorWithRegistry(1, reg)
	te.Start()
	defer te.Stop()

	root := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx, fc := ctxapi.OpenFrameContext(root)
	defer ctxapi.ReleaseFrameContext(fc)
	if err := fc.Set(upgradeInheritedKey, "keep"); err != nil {
		t.Fatalf("set inherited frame value: %v", err)
	}
	fc.Seal()

	process.WithFactory(ctx, &mockFactory{
		createFunc: func(_ registry.ID) (process.Process, *process.Meta, error) {
			return &FrameWritingProcess{}, &process.Meta{Method: "run"}, nil
		},
	})

	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pid := pidapi.PID{UniqID: "upgrade-sealed-frame"}
	proc := &UpgradeProcess{
		upgradeReq: &process.UpgradeRequest{
			Source: registry.ID{Name: "test"},
		},
	}

	result, err := te.Execute(runCtx, pid, proc, "", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
}

// TestUpgradeSuccess_Stress guards against missed wakeup regressions by repeatedly
// executing upgrade flow on a single-worker scheduler.
func TestUpgradeSuccess_Stress(t *testing.T) {
	reg := scheduler.NewRegistry()
	te := newTestExecutorWithRegistry(1, reg)
	te.Start()
	defer te.Stop()

	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	process.WithFactory(ctx, &mockFactory{
		createFunc: func(_ registry.ID) (process.Process, *process.Meta, error) {
			return &ImmediateProcess{}, &process.Meta{Method: "run"}, nil
		},
	})

	const runs = 300
	for i := 0; i < runs; i++ {
		runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		p := pidapi.PID{UniqID: fmt.Sprintf("upgrade-stress-%d", i)}
		proc := &UpgradeProcess{
			upgradeReq: &process.UpgradeRequest{
				Source: registry.ID{Name: "test"},
			},
		}

		result, err := te.Execute(runCtx, p, proc, "", nil)
		cancel()
		if err != nil {
			t.Fatalf("run %d execute error: %v", i, err)
		}
		if result.Error != nil {
			t.Fatalf("run %d unexpected error: %v", i, result.Error)
		}
	}
}

// UnknownYieldProcess yields unknown command
type UnknownYieldProcess struct {
	done bool
}

type UnknownCmd struct{}

func (UnknownCmd) CmdID() dispatcher.CommandID { return 999 }

func (p *UnknownYieldProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *UnknownYieldProcess) Step(events []process.Event, out *process.StepOutput) error {
	if p.done {
		for _, e := range events {
			if e.Type == process.EventYieldComplete && e.Error != nil {
				return e.Error
			}
		}
		out.Done(nil)
		return nil
	}
	p.done = true
	out.Yield(UnknownCmd{}, 0)
	out.Continue()
	return nil
}

func (p *UnknownYieldProcess) Send(*relay.Package) error { return nil }
func (p *UnknownYieldProcess) Close()                    {}

// TestUnknownCommandHandler tests yield with unregistered command
func TestUnknownCommandHandler(t *testing.T) {
	reg := scheduler.NewRegistry()
	te := newTestExecutorWithRegistry(1, reg)
	te.Start()
	defer te.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pid := pidapi.PID{UniqID: "unknown-cmd"}
	result, err := te.Execute(ctx, pid, &UnknownYieldProcess{}, "", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Error == nil {
		t.Fatal("expected error for unknown command")
	}
}

// FailingHandler returns error from Handle
type FailingHandler struct{}

func (h *FailingHandler) Handle(_ context.Context, _ dispatcher.Command, _ uint64, _ dispatcher.ResultReceiver) error {
	return errors.New("handler failed")
}

// HandlerErrorProcess tests handler error
type HandlerErrorProcess struct {
	done bool
}

func (p *HandlerErrorProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *HandlerErrorProcess) Step(events []process.Event, out *process.StepOutput) error {
	if p.done {
		for _, e := range events {
			if e.Type == process.EventYieldComplete && e.Error != nil {
				return e.Error
			}
		}
		out.Done(nil)
		return nil
	}
	p.done = true
	out.Yield(YieldCmd{}, 0)
	out.Continue()
	return nil
}

func (p *HandlerErrorProcess) Send(*relay.Package) error { return nil }
func (p *HandlerErrorProcess) Close()                    {}

// TestHandlerError tests handler returning error
func TestHandlerError(t *testing.T) {
	reg := scheduler.NewRegistry()
	reg.Register(CmdYield, &FailingHandler{})
	te := newTestExecutorWithRegistry(1, reg)
	te.Start()
	defer te.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pid := pidapi.PID{UniqID: "handler-error"}
	result, err := te.Execute(ctx, pid, &HandlerErrorProcess{}, "", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Error == nil {
		t.Fatal("expected error from handler")
	}
	if result.Error.Error() != "handler failed" {
		t.Fatalf("expected 'handler failed', got %v", result.Error)
	}
}

// TestMaxProcessesLimit tests process limit enforcement
func TestMaxProcessesLimit(t *testing.T) {
	sched := NewScheduler(scheduler.NewRegistry(), WithWorkers(1), WithMaxProcesses(2))
	// Don't start to prevent completion

	pid1 := pidapi.PID{UniqID: "limit-1"}
	pid2 := pidapi.PID{UniqID: "limit-2"}
	pid3 := pidapi.PID{UniqID: "limit-3"}

	_, err := sched.Submit(context.Background(), pid1, &ImmediateProcess{}, "", nil)
	if err != nil {
		t.Fatalf("submit 1 error: %v", err)
	}

	_, err = sched.Submit(context.Background(), pid2, &ImmediateProcess{}, "", nil)
	if err != nil {
		t.Fatalf("submit 2 error: %v", err)
	}

	// Third should fail
	_, err = sched.Submit(context.Background(), pid3, &ImmediateProcess{}, "", nil)
	if err == nil {
		t.Fatal("expected max processes error")
	}
	if !errors.Is(err, process.ErrMaxProcessesExceeded) {
		t.Fatalf("expected ErrMaxProcessesExceeded, got %v", err)
	}
}

// TestSubmitAfterStopping tests submit rejection after Stop
func TestSubmitAfterStopping(t *testing.T) {
	sched := newTestScheduler(1)
	sched.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sched.Stop(ctx)

	pid := pidapi.PID{UniqID: "after-stop"}
	_, err := sched.Submit(context.Background(), pid, &ImmediateProcess{}, "", nil)
	if err == nil {
		t.Fatal("expected error after stop")
	}
	if !errors.Is(err, process.ErrSchedulerStopping) {
		t.Fatalf("expected ErrSchedulerStopping, got %v", err)
	}
}

// TestCreateProcessorMaxLimit tests CreateProcessor with max processes
func TestCreateProcessorMaxLimit(t *testing.T) {
	sched := NewScheduler(scheduler.NewRegistry(), WithWorkers(1), WithMaxProcesses(1))
	sched.Start()
	defer testStopScheduler(sched)

	pid1 := pidapi.PID{UniqID: "create-limit-1"}
	_, err := sched.CreateProcessor(context.Background(), pid1, &ImmediateProcess{})
	if err != nil {
		t.Fatalf("create 1 error: %v", err)
	}

	pid2 := pidapi.PID{UniqID: "create-limit-2"}
	_, err = sched.CreateProcessor(context.Background(), pid2, &ImmediateProcess{})
	if err == nil {
		t.Fatal("expected max processes error")
	}
	if !errors.Is(err, process.ErrMaxProcessesExceeded) {
		t.Fatalf("expected ErrMaxProcessesExceeded, got %v", err)
	}
}

// StepErrorProcess returns error from Step
type StepErrorProcess struct{}

func (p *StepErrorProcess) Init(_ context.Context, _ string, _ payload.Payloads) error { return nil }
func (p *StepErrorProcess) Step(_ []process.Event, _ *process.StepOutput) error {
	return errors.New("step failed")
}
func (p *StepErrorProcess) Send(*relay.Package) error { return nil }
func (p *StepErrorProcess) Close()                    {}

// TestStepError tests process Step returning error
func TestStepError(t *testing.T) {
	reg := scheduler.NewRegistry()
	reg.Register(CmdComplete, CompleteHandler())
	reg.Register(CmdYield, YieldHandler())
	te := newTestExecutorWithRegistry(1, reg)
	te.Start()
	defer te.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pid := pidapi.PID{UniqID: "step-error"}
	result, err := te.Execute(ctx, pid, &StepErrorProcess{}, "", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Error == nil || result.Error.Error() != "step failed" {
		t.Fatalf("expected 'step failed' error, got %v", result.Error)
	}
}

// InitErrorProcess fails during Init
type InitErrorProcess struct{}

func (p *InitErrorProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return errors.New("init failed")
}
func (p *InitErrorProcess) Step(_ []process.Event, _ *process.StepOutput) error { return nil }
func (p *InitErrorProcess) Send(*relay.Package) error                           { return nil }
func (p *InitErrorProcess) Close()                                              {}

// TestInitError tests Submit with failing Init
func TestInitError(t *testing.T) {
	sched := newTestScheduler(1)
	sched.Start()
	defer testStopScheduler(sched)

	pid := pidapi.PID{UniqID: "init-error"}
	_, err := sched.Submit(context.Background(), pid, &InitErrorProcess{}, "", nil)
	if err == nil {
		t.Fatal("expected error from Init")
	}
	if err.Error() != "init failed" {
		t.Fatalf("expected 'init failed', got %v", err)
	}
}

// TestLifecycleOnStartError tests lifecycle rejecting process start
func TestLifecycleOnStartError(t *testing.T) {
	reg := scheduler.NewRegistry()
	sched := NewScheduler(reg, WithWorkers(1), WithLifecycle(&rejectingLifecycle2{}))
	sched.Start()
	defer testStopScheduler(sched)

	pid := pidapi.PID{UniqID: "lifecycle-reject"}
	_, err := sched.Submit(context.Background(), pid, &ImmediateProcess{}, "", nil)
	if err == nil {
		t.Fatal("expected error from lifecycle")
	}
	if err.Error() != "rejected" {
		t.Fatalf("expected 'rejected', got %v", err)
	}
}

type rejectingLifecycle2 struct{}

func (l *rejectingLifecycle2) OnStart(_ context.Context, _ pidapi.PID, _ process.Process) error {
	return errors.New("rejected")
}

func (l *rejectingLifecycle2) OnComplete(_ context.Context, _ pidapi.PID, _ *runtime.Result) {}

// TestInjectQueueDrain tests draining inject queue
func TestInjectQueueDrain(t *testing.T) {
	q := NewInjectQueue()

	// Push several items
	for i := 0; i < 10; i++ {
		q.Push(&Processor{id: uint64(i)})
	}

	// Drain into buffer
	buf := make([]*Processor, 5)
	n := q.Drain(buf)
	if n != 5 {
		t.Fatalf("expected 5 drained, got %d", n)
	}

	// Drain remaining
	n = q.Drain(buf)
	if n != 5 {
		t.Fatalf("expected 5 more drained, got %d", n)
	}

	// Should be empty now
	n = q.Drain(buf)
	if n != 0 {
		t.Fatalf("expected 0 drained, got %d", n)
	}
}

// TestWorkerStealEmpty tests stealing from empty workers
func TestWorkerStealEmpty(t *testing.T) {
	sched := newTestScheduler(4)
	// Don't start, just test steal logic

	w := sched.workers[0]
	proc := w.steal()
	if proc != nil {
		t.Fatal("expected nil from empty steal")
	}
}

// TestCollectProcessStatsEmpty tests stats collection with no stats providers
func TestCollectProcessStatsEmpty(t *testing.T) {
	sched := newTestScheduler(1)
	sched.Start()
	defer testStopScheduler(sched)

	// Submit process without stats
	pid := pidapi.PID{UniqID: "no-stats"}
	_, err := sched.Submit(context.Background(), pid, &ImmediateProcess{}, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	stats := sched.CollectProcessStats()
	// Should be empty since ImmediateProcess doesn't implement StatsProvider
	if len(stats) != 0 {
		t.Fatalf("expected 0 stats, got %d", len(stats))
	}
}

// TestSendToNonexistentProcess tests Send to unknown PID
func TestSendToNonexistentProcess(t *testing.T) {
	sched := newTestScheduler(1)
	sched.Start()
	defer testStopScheduler(sched)

	err := sched.Send(&relay.Package{Target: pidapi.PID{UniqID: "nonexistent"}})
	if !errors.Is(err, process.ErrProcessNotFound) {
		t.Fatalf("expected ErrProcessNotFound, got %v", err)
	}
}

// TestWorkerStatsMultiple tests WorkerStats with multiple workers
func TestWorkerStatsMultiple(t *testing.T) {
	var completed atomic.Int32

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Add(1)
		},
	}

	sched := newTestSchedulerWithLifecycle(4, lc)
	sched.Start()
	defer testStopScheduler(sched)

	// Submit many processes to distribute across workers
	for i := 0; i < 100; i++ {
		pid := pidapi.PID{UniqID: fmt.Sprintf("multi-%d", i)}
		_, _ = sched.Submit(context.Background(), pid, &CounterProcess{}, "", testInput(5))
	}

	deadline := time.Now().Add(5 * time.Second)
	for completed.Load() < 100 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	stats := sched.WorkerStats()
	if len(stats) != 4 {
		t.Fatalf("expected 4 worker stats, got %d", len(stats))
	}

	// Check that work was distributed
	var totalExecuted uint64
	for _, ws := range stats {
		totalExecuted += ws["executed"]
	}

	if totalExecuted == 0 {
		t.Fatal("expected non-zero total executed")
	}
}

// TestStatsProcess implements process.StatsProvider for testing
type TestStatsProcess struct {
	done bool
}

func (p *TestStatsProcess) Init(_ context.Context, _ string, _ payload.Payloads) error { return nil }
func (p *TestStatsProcess) Step(_ []process.Event, out *process.StepOutput) error {
	if p.done {
		out.Done(nil)
		return nil
	}
	p.done = true
	out.Idle()
	return nil
}
func (p *TestStatsProcess) Send(*relay.Package) error { return nil }
func (p *TestStatsProcess) Close()                    {}
func (p *TestStatsProcess) Stats() attrs.Attributes {
	b := attrs.NewBag()
	b.Set("test_key", "test_value")
	return b
}

// TestCollectProcessStatsWithProvider tests stats collection with a stats provider
func TestCollectProcessStatsWithProvider(t *testing.T) {
	sched := newTestScheduler(1)
	sched.Start()
	defer testStopScheduler(sched)

	pid := pidapi.PID{UniqID: "stats-provider"}
	_, err := sched.Submit(context.Background(), pid, &TestStatsProcess{}, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait for process to be running
	time.Sleep(50 * time.Millisecond)

	stats := sched.CollectProcessStats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 stats, got %d", len(stats))
	}

	val, ok := stats[0].Get("test_key")
	if !ok || val != "test_value" {
		t.Fatalf("expected test_value, got %v", val)
	}
}

// TestWakeProcessorPath tests the WakeProcessor mechanism via YieldCompleter
func TestWakeProcessorPath(t *testing.T) {
	reg := scheduler.NewRegistry()
	reg.Register(CmdYield, dispatcher.HandlerFunc(func(_ context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		proc := receiver.(*Processor)
		completer := proc.queue.NewYieldCompleter(proc.scheduler)
		go func() {
			time.Sleep(10 * time.Millisecond)
			completer.CompleteYield(tag, nil, nil)
		}()
		return nil
	}))

	te := newTestExecutorWithRegistry(1, reg)
	te.Start()
	defer te.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pid := pidapi.PID{UniqID: "wake-processor"}
	_, err := te.Execute(ctx, pid, &YieldingProcess{count: 1}, "", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
}

// YieldingProcess yields N times then completes
type YieldingProcess struct {
	count   int
	current int
}

func (p *YieldingProcess) Init(_ context.Context, _ string, _ payload.Payloads) error { return nil }
func (p *YieldingProcess) Step(events []process.Event, out *process.StepOutput) error {
	for range events {
		p.current++
	}
	if p.current >= p.count {
		out.Done(nil)
		return nil
	}
	out.Yield(YieldCmd{}, 0)
	out.Continue()
	return nil
}
func (p *YieldingProcess) Send(*relay.Package) error { return nil }
func (p *YieldingProcess) Close()                    {}

// TestWithLocalQueueSize tests the WithLocalQueueSize option
func TestWithLocalQueueSize(t *testing.T) {
	sched := NewScheduler(scheduler.NewRegistry(), WithWorkers(1), WithLocalQueueSize(512))
	// Just verify it doesn't panic and works
	if sched.localQueueSize != 512 {
		t.Fatalf("expected localQueueSize 512, got %d", sched.localQueueSize)
	}
}

// TestWakeProcessorStaleGeneration tests WakeProcessor with stale generation
func TestWakeProcessorStaleGeneration(t *testing.T) {
	sched := newTestScheduler(1)
	sched.Start()
	defer testStopScheduler(sched)

	// Create a queue with known generation
	q := process.NewEventQueue()
	oldGen := q.Generation()

	// Reset to get new generation
	q.Reset()
	newGen := q.Generation()

	if oldGen == newGen {
		t.Fatal("expected different generations after reset")
	}

	// WakeProcessor with stale generation should no-op (not crash)
	sched.WakeProcessor(q, oldGen)
}

// TestWakeProcessorUnknownQueue tests WakeProcessor with unknown queue
func TestWakeProcessorUnknownQueue(t *testing.T) {
	sched := newTestScheduler(1)
	sched.Start()
	defer testStopScheduler(sched)

	// Create a queue not registered with scheduler
	q := process.NewEventQueue()

	// WakeProcessor with unknown queue should no-op (not crash)
	sched.WakeProcessor(q, q.Generation())
}
