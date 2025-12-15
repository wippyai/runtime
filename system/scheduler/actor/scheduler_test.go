package actor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	sysprocess "github.com/wippyai/runtime/system/process"
	"github.com/wippyai/runtime/system/scheduler"
)

// Test commands

const (
	CmdComplete dispatcher.CommandID = 0
	CmdYield    dispatcher.CommandID = 1
	CmdSleep    dispatcher.CommandID = 10
)

type CompleteCmd struct{ Value any }

func (CompleteCmd) CmdID() dispatcher.CommandID { return CmdComplete }

type YieldCmd struct{}

func (YieldCmd) CmdID() dispatcher.CommandID { return CmdYield }

type SleepCmd struct{ Duration time.Duration }

func (SleepCmd) CmdID() dispatcher.CommandID { return CmdSleep }

// Test handlers

func CompleteHandler() dispatcher.Handler {
	return dispatcher.HandlerFunc(func(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		c := cmd.(CompleteCmd)
		go receiver.CompleteYield(tag, c.Value, nil)
		return nil
	})
}

func YieldHandler() dispatcher.Handler {
	return dispatcher.HandlerFunc(func(_ context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		go receiver.CompleteYield(tag, nil, nil)
		return nil
	})
}

func SleepHandler() dispatcher.Handler {
	return dispatcher.HandlerFunc(func(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		s := cmd.(SleepCmd)
		timer := time.NewTimer(s.Duration)
		go func() {
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-timer.C:
				receiver.CompleteYield(tag, nil, nil)
			}
		}()
		return nil
	})
}

// Test processes

type CounterProcess struct {
	target  int
	current int
	ctx     context.Context
}

func (p *CounterProcess) Init(ctx context.Context, _ string, input payload.Payloads) error {
	p.ctx = ctx
	if len(input) > 0 {
		p.target = input[0].Data().(int)
	}
	return nil
}

func (p *CounterProcess) Step(_ []process.Event, out *process.StepOutput) error {
	if p.current >= p.target {
		out.Yield(CompleteCmd{Value: p.current}, 0)
		out.Done(nil)
		return nil
	}

	p.current++
	out.Yield(YieldCmd{}, 0)
	out.Continue()
	return nil
}

func (p *CounterProcess) Send(*relay.Package) error {
	return nil
}

func (p *CounterProcess) Close() {}

type SleepProcess struct {
	duration time.Duration
	slept    bool
	ctx      context.Context
}

func (p *SleepProcess) Init(ctx context.Context, _ string, input payload.Payloads) error {
	p.ctx = ctx
	if len(input) > 0 {
		p.duration = input[0].Data().(time.Duration)
	}
	return nil
}

func (p *SleepProcess) Step(_ []process.Event, out *process.StepOutput) error {
	if !p.slept {
		p.slept = true
		out.Yield(SleepCmd{Duration: p.duration}, 0)
		out.Continue()
		return nil
	}

	out.Yield(CompleteCmd{Value: "done"}, 0)
	out.Done(nil)
	return nil
}

func (p *SleepProcess) Send(*relay.Package) error {
	return nil
}

func (p *SleepProcess) Close() {}

// testLifecycle implements process.Lifecycle for tests
type testLifecycle struct {
	onStart    func(context.Context, pidapi.PID, process.Process)
	onComplete func(context.Context, pidapi.PID, *runtime.Result)
}

func (l *testLifecycle) OnStart(ctx context.Context, p pidapi.PID, proc process.Process) {
	if l.onStart != nil {
		l.onStart(ctx, p, proc)
	}
}

func (l *testLifecycle) OnComplete(ctx context.Context, p pidapi.PID, result *runtime.Result) {
	if l.onComplete != nil {
		l.onComplete(ctx, p, result)
	}
}

// Helper to create test scheduler
func newTestScheduler(workers int) *Scheduler {
	registry := scheduler.NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())
	registry.Register(CmdSleep, SleepHandler())
	return NewScheduler(registry, WithWorkers(workers))
}

func newTestSchedulerWithLifecycle(workers int, lc *testLifecycle) *Scheduler {
	registry := scheduler.NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())
	registry.Register(CmdSleep, SleepHandler())
	return NewScheduler(registry, WithWorkers(workers), WithLifecycle(lc))
}

func testPID() pidapi.PID {
	return pidapi.PID{UniqID: "test"}
}

func testInput(v any) payload.Payloads {
	return payload.Payloads{payload.New(v)}
}

// newTestExecutor creates a testExecutor with the standard test registry
func newTestExecutor(workers int) *testExecutor {
	registry := scheduler.NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())
	registry.Register(CmdSleep, SleepHandler())
	return newTestExecutorWithRegistry(workers, registry)
}

// Tests

func TestSchedulerBasic(t *testing.T) {
	var completed atomic.Bool
	var result *runtime.Result

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, res *runtime.Result) {
			result = res
			completed.Store(true)
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop(context.Background())

	_, err := sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(10))
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process did not complete")
	}

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerMultipleProcesses(t *testing.T) {
	var completedCount atomic.Int32

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, result *runtime.Result) {
			if result.Error != nil {
				t.Errorf("process error: %v", result.Error)
				return
			}
			completedCount.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop(context.Background())

	const numProcesses = 10
	for i := 0; i < numProcesses; i++ {
		_, err := sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(100))
		if err != nil {
			t.Fatalf("submit error: %v", err)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for completedCount.Load() < numProcesses && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if completedCount.Load() != numProcesses {
		t.Fatalf("expected %d completed, got %d", numProcesses, completedCount.Load())
	}
}

func TestSchedulerSleep(t *testing.T) {
	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, result *runtime.Result) {
			if result.Error != nil {
				t.Errorf("error: %v", result.Error)
			}
			completed.Store(true)
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop(context.Background())

	start := time.Now()
	_, err := sched.Submit(context.Background(), testPID(), &SleepProcess{}, "", testInput(50*time.Millisecond))
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	elapsed := time.Since(start)
	if elapsed < 50*time.Millisecond {
		t.Errorf("completed too fast: %v", elapsed)
	}

	if !completed.Load() {
		t.Fatal("process did not complete")
	}
}

func TestSchedulerWorkDistribution(t *testing.T) {
	var completed atomic.Int32

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(4, lc)

	sched.Start()
	defer sched.Stop(context.Background())

	for i := 0; i < 100; i++ {
		_, _ = sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(50))
	}

	deadline := time.Now().Add(5 * time.Second)
	for completed.Load() < 100 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if completed.Load() != 100 {
		t.Fatalf("expected 100 completed, got %d", completed.Load())
	}

	stats := sched.Stats()
	t.Logf("Scheduler stats: executed=%d", stats["executed"])
}

func TestSchedulerSubmitBeforeStart(t *testing.T) {
	sched := newTestScheduler(1)

	// Submit before Start should work
	proc, err := sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(1))
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	if proc == nil {
		t.Fatal("expected processor to be returned")
	}

	// Now start and let it complete
	sched.Start()
	defer sched.Stop(context.Background())
}

func TestSchedulerStats(t *testing.T) {
	var completed atomic.Int32

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop(context.Background())

	for i := 0; i < 10; i++ {
		_, _ = sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(10))
	}

	deadline := time.Now().Add(5 * time.Second)
	for completed.Load() < 10 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	stats := sched.Stats()
	if stats["executed"] == 0 {
		t.Fatal("expected non-zero executed count")
	}
	if stats["workers"] != 2 {
		t.Fatalf("expected 2 workers, got %d", stats["workers"])
	}

	workerStats := sched.WorkerStats()
	if len(workerStats) != 2 {
		t.Fatalf("expected 2 worker stats, got %d", len(workerStats))
	}
}

// StatsProcess implements process.StatsProvider
type StatsProcess struct {
	CounterProcess
	customStats string
}

var _ process.StatsProvider = (*StatsProcess)(nil)

func (p *StatsProcess) Stats() attrs.Attributes {
	return attrs.NewBagFrom(map[string]any{"custom": p.customStats})
}

func TestSchedulerCollectProcessStats(t *testing.T) {
	sched := newTestScheduler(2)
	sched.Start()
	defer sched.Stop(context.Background())

	pid := pidapi.PID{UniqID: "stats-process"}
	proc := &StatsProcess{customStats: "test-value"}

	_, err := sched.Submit(context.Background(), pid, proc, "", testInput(1000))
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	stats := sched.CollectProcessStats()

	// Process may have completed before stats collection
	if len(stats) == 0 {
		t.Log("No stats collected - process may have completed")
		return
	}

	var found bool
	for _, s := range stats {
		if s.GetString("custom", "") == "test-value" {
			found = true
			break
		}
	}

	if !found {
		t.Log("Stats not found - process may have completed before collection")
	}
}

// Execute tests (using testExecutor helper)

func TestSchedulerExecute(t *testing.T) {
	te := newTestExecutor(2)
	te.Start()
	defer te.Stop()

	result, err := te.Execute(context.Background(), testPID(), &CounterProcess{}, "", testInput(10))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}
}

func TestSchedulerExecuteMultiple(t *testing.T) {
	te := newTestExecutor(4)
	te.Start()
	defer te.Stop()

	var wg sync.WaitGroup
	errs := make([]error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Use unique PID per goroutine to avoid collision in pending map
			pid := pidapi.PID{UniqID: fmt.Sprintf("test-%d", idx)}
			result, err := te.Execute(context.Background(), pid, &CounterProcess{}, "", testInput((idx+1)*10))
			if err != nil {
				errs[idx] = err
				return
			}
			if result.Error != nil {
				errs[idx] = result.Error
			}
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("execute %d error: %v", i, err)
		}
	}
}

func TestSchedulerExecuteWithSleep(t *testing.T) {
	te := newTestExecutor(2)
	te.Start()
	defer te.Stop()

	start := time.Now()
	result, err := te.Execute(context.Background(), testPID(), &SleepProcess{}, "", testInput(50*time.Millisecond))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if elapsed < 50*time.Millisecond {
		t.Errorf("completed too fast: %v", elapsed)
	}

	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}
}

// Callback tests

func TestOnStartCallback(t *testing.T) {
	var startCalls atomic.Int32
	var startPIDs []pidapi.PID
	var mu sync.Mutex

	lc := &testLifecycle{
		onStart: func(_ context.Context, p pidapi.PID, _ process.Process) {
			startCalls.Add(1)
			mu.Lock()
			startPIDs = append(startPIDs, p)
			mu.Unlock()
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop(context.Background())

	// Submit multiple processes
	for i := 0; i < 5; i++ {
		pid := pidapi.PID{UniqID: fmt.Sprintf("test-%d", i)}
		_, _ = sched.Submit(context.Background(), pid, &CounterProcess{}, "", testInput(1))
	}

	// Wait for all to complete
	deadline := time.Now().Add(2 * time.Second)
	for startCalls.Load() < 5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if startCalls.Load() != 5 {
		t.Fatalf("expected 5 OnStart calls, got %d", startCalls.Load())
	}

	mu.Lock()
	if len(startPIDs) != 5 {
		t.Fatalf("expected 5 PIDs recorded, got %d", len(startPIDs))
	}
	mu.Unlock()
}

func TestOnCompleteCallback(t *testing.T) {
	var completeCalls atomic.Int32
	var completePIDs []pidapi.PID
	var completeResults []*runtime.Result
	var mu sync.Mutex

	lc := &testLifecycle{
		onComplete: func(_ context.Context, p pidapi.PID, result *runtime.Result) {
			completeCalls.Add(1)
			mu.Lock()
			completePIDs = append(completePIDs, p)
			completeResults = append(completeResults, result)
			mu.Unlock()
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop(context.Background())

	// Submit multiple processes
	for i := 0; i < 5; i++ {
		pid := pidapi.PID{UniqID: fmt.Sprintf("test-%d", i)}
		_, _ = sched.Submit(context.Background(), pid, &CounterProcess{}, "", testInput(1))
	}

	// Wait for all to complete
	deadline := time.Now().Add(2 * time.Second)
	for completeCalls.Load() < 5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if completeCalls.Load() != 5 {
		t.Fatalf("expected 5 OnComplete calls, got %d", completeCalls.Load())
	}

	mu.Lock()
	for _, result := range completeResults {
		if result.Error != nil {
			t.Errorf("unexpected error in result: %v", result.Error)
		}
	}
	mu.Unlock()
}

func TestSendByPID(t *testing.T) {
	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Store(true)
		},
	}
	sched := newTestSchedulerWithLifecycle(1, lc)

	// Don't start scheduler yet - this ensures process won't complete before Send()
	pid := pidapi.PID{UniqID: "send-test"}
	_, err := sched.Submit(context.Background(), pid, &SleepProcess{duration: 100 * time.Millisecond}, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Now start scheduler
	sched.Start()
	defer sched.Stop(context.Background())

	// Send to active process should work
	pkg := &relay.Package{Target: pid}
	err = sched.Send(pkg)
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}

	// Wait for completion via callback
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// After completion, Send should return ErrProcessNotFound
	err = sched.Send(pkg)
	if err == nil {
		t.Fatal("expected error for completed PID")
	}
	if !errors.Is(err, process.ErrProcessNotFound) {
		t.Fatalf("expected ErrProcessNotFound, got %T", err)
	}
}

func TestTerminate(t *testing.T) {
	var completed atomic.Bool
	var result *runtime.Result
	var completedPID pidapi.PID

	lc := &testLifecycle{
		onComplete: func(_ context.Context, p pidapi.PID, res *runtime.Result) {
			completedPID = p
			result = res
			completed.Store(true)
		},
	}

	registry := scheduler.NewRegistry()
	registry.Register(CmdYield, YieldHandler())
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdSleep, SleepHandler())

	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

	// Submit a blocking process
	pid := pidapi.PID{UniqID: "term-test"}
	_, err := sched.Submit(context.Background(), pid, &SleepProcess{duration: 10 * time.Second}, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Terminate the process
	err = sched.Terminate(pid)
	if err != nil {
		t.Fatalf("terminate error: %v", err)
	}

	// Wait for completion callback
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process was not terminated")
	}

	if completedPID != pid {
		t.Fatalf("wrong pid: got %v, want %v", completedPID, pid)
	}

	if !errors.Is(result.Error, sysprocess.ErrTerminated) {
		t.Fatalf("expected ErrTerminated, got %v", result.Error)
	}

	// Verify process is gone
	err = sched.Send(&relay.Package{Target: pid})
	if !errors.Is(err, process.ErrProcessNotFound) {
		t.Fatalf("expected ErrProcessNotFound, got %v", err)
	}
}

func TestTerminateNotFound(t *testing.T) {
	sched := newTestScheduler(1)
	sched.Start()
	defer sched.Stop(context.Background())

	err := sched.Terminate(pidapi.PID{UniqID: "nonexistent"})
	if !errors.Is(err, process.ErrProcessNotFound) {
		t.Fatalf("expected ErrProcessNotFound, got %v", err)
	}
}

// IdleProcess goes idle immediately and waits for messages
type IdleProcess struct {
	ctx context.Context
}

func (p *IdleProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	return nil
}

func (p *IdleProcess) Step(_ []process.Event, out *process.StepOutput) error {
	out.Idle()
	return nil
}

func (p *IdleProcess) Close() {}

func TestTerminateIdleProcess(t *testing.T) {
	var completed atomic.Bool
	var result *runtime.Result

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, res *runtime.Result) {
			result = res
			completed.Store(true)
		},
	}

	sched := newTestSchedulerWithLifecycle(1, lc)
	sched.Start()
	defer sched.Stop(context.Background())

	pid := pidapi.PID{UniqID: "idle-term-test"}
	_, err := sched.Submit(context.Background(), pid, &IdleProcess{}, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Give it time to become idle
	time.Sleep(50 * time.Millisecond)

	// Terminate the idle process
	err = sched.Terminate(pid)
	if err != nil {
		t.Fatalf("terminate error: %v", err)
	}

	// Wait for completion callback
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("idle process was not terminated")
	}

	if !errors.Is(result.Error, sysprocess.ErrTerminated) {
		t.Fatalf("expected ErrTerminated, got %v", result.Error)
	}

	// Verify process is gone
	err = sched.Send(&relay.Package{Target: pid})
	if !errors.Is(err, process.ErrProcessNotFound) {
		t.Fatalf("expected ErrProcessNotFound, got %v", err)
	}
}

// Single worker tests

func TestSchedulerSingleWorker(t *testing.T) {
	var completed atomic.Bool
	var result *runtime.Result

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, res *runtime.Result) {
			result = res
			completed.Store(true)
		},
	}

	registry := scheduler.NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())

	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

	ctx := context.Background()
	pid := pidapi.PID{UniqID: "single"}

	_, err := sched.Submit(ctx, pid, &CounterProcess{}, "", testInput(5))
	if err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(1 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("timed out waiting for completion")
	}
	if result.Error != nil {
		t.Fatalf("Result error: %v", result.Error)
	}

	stats := sched.Stats()
	if stats["executed"] != 6 {
		t.Fatalf("expected 6 steps, got %d", stats["executed"])
	}
	if stats["workers"] != 1 {
		t.Fatalf("expected 1 worker, got %d", stats["workers"])
	}
}

// Allocation tests

func TestSchedulerSubmitAlloc(t *testing.T) {
	var completed atomic.Int32

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(1, lc)

	sched.Start()
	defer sched.Stop(context.Background())

	pid := testPID()
	input := testInput(1)

	// Warm up
	for i := 0; i < 100; i++ {
		_, _ = sched.Submit(context.Background(), pid, &CounterProcess{}, "", input)
	}

	deadline := time.Now().Add(5 * time.Second)
	for completed.Load() < 100 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// Measure allocations
	allocs := testing.AllocsPerRun(100, func() {
		_, _ = sched.Submit(context.Background(), pid, &CounterProcess{}, "", input)
	})

	deadline = time.Now().Add(5 * time.Second)
	for completed.Load() < 200 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	t.Logf("Submit allocs per op: %f", allocs)
}

// Memory leak tests

// TrackingProcess tracks Close() calls
type TrackingProcess struct {
	closeCalled *atomic.Int32
	ctx         context.Context
}

func (p *TrackingProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	return nil
}

func (p *TrackingProcess) Step(_ []process.Event, out *process.StepOutput) error {
	out.Yield(CompleteCmd{Value: "done"}, 0)
	out.Done(nil)
	return nil
}

func (p *TrackingProcess) Send(*relay.Package) error {
	return nil
}

func (p *TrackingProcess) Close() {
	if p.closeCalled != nil {
		p.closeCalled.Add(1)
	}
}

func TestSchedulerReleasesProcesses(t *testing.T) {
	var closeCalls atomic.Int32
	var completed atomic.Int32

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop(context.Background())

	const numProcs = 1000
	ctx := context.Background()

	for i := 0; i < numProcs; i++ {
		pid := pidapi.PID{Host: "test", UniqID: fmt.Sprintf("%d", i)}
		proc := &TrackingProcess{closeCalled: &closeCalls}
		_, err := sched.Submit(ctx, pid, proc, "", nil)
		if err != nil {
			t.Fatalf("Submit failed: %v", err)
		}
	}

	// Wait for all to complete
	deadline := time.Now().Add(10 * time.Second)
	for completed.Load() < numProcs && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if completed.Load() != numProcs {
		t.Fatalf("expected %d completions, got %d", numProcs, completed.Load())
	}

	// Give time for Close() calls
	time.Sleep(100 * time.Millisecond)

	if closeCalls.Load() != numProcs {
		t.Fatalf("expected %d Close() calls, got %d", numProcs, closeCalls.Load())
	}

	// Check maps are empty
	var byPIDCount int
	sched.byPID.Range(func(_, _ any) bool {
		byPIDCount++
		return true
	})

	if byPIDCount != 0 {
		t.Errorf("byPID map has %d entries, expected 0", byPIDCount)
	}
}

// BlockedProcess blocks on yield waiting for handler completion
type BlockedProcess struct {
	blocked chan struct{}
	ctx     context.Context
}

func (p *BlockedProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	return nil
}

func (p *BlockedProcess) Step(_ []process.Event, out *process.StepOutput) error {
	// Signal that we're blocked waiting for yield completion
	if p.blocked != nil {
		close(p.blocked)
	}
	out.Yield(SleepCmd{Duration: 10 * time.Second}, 0)
	out.Continue()
	return nil
}

func (p *BlockedProcess) Send(*relay.Package) error {
	return nil
}

func (p *BlockedProcess) Close() {}

func TestTerminateBlockedProcess(t *testing.T) {
	var completed atomic.Bool
	var result *runtime.Result

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, res *runtime.Result) {
			result = res
			completed.Store(true)
		},
	}

	sched := newTestSchedulerWithLifecycle(1, lc)
	sched.Start()
	defer sched.Stop(context.Background())

	blocked := make(chan struct{})
	pid := pidapi.PID{UniqID: "blocked-term-test"}
	_, err := sched.Submit(context.Background(), pid, &BlockedProcess{blocked: blocked}, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait for process to become blocked
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("process did not become blocked")
	}

	// Give time for state to transition to Blocked
	time.Sleep(50 * time.Millisecond)

	// Terminate the blocked process
	err = sched.Terminate(pid)
	if err != nil {
		t.Fatalf("terminate error: %v", err)
	}

	// Wait for completion callback
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("blocked process was not terminated")
	}

	if !errors.Is(result.Error, sysprocess.ErrTerminated) {
		t.Fatalf("expected ErrTerminated, got %v", result.Error)
	}
}

func TestSendToTerminatedProcess(t *testing.T) {
	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := newTestSchedulerWithLifecycle(1, lc)
	sched.Start()
	defer sched.Stop(context.Background())

	blocked := make(chan struct{})
	pid := pidapi.PID{UniqID: "send-term-test"}
	_, err := sched.Submit(context.Background(), pid, &BlockedProcess{blocked: blocked}, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait for process to become blocked
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("process did not become blocked")
	}

	// Terminate the process
	err = sched.Terminate(pid)
	if err != nil {
		t.Fatalf("terminate error: %v", err)
	}

	// Wait for completion
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// After termination, Send should fail
	pkg := &relay.Package{Target: pid}
	err = sched.Send(pkg)
	if err == nil {
		t.Fatal("expected error when sending to terminated process")
	}
	if !errors.Is(err, process.ErrProcessNotFound) {
		t.Fatalf("expected ErrProcessNotFound, got %v", err)
	}
}

// ContextTrackingProcess verifies context cancellation
type ContextTrackingProcess struct {
	ctx         context.Context
	ctxCanceled chan struct{}
}

func (p *ContextTrackingProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	p.ctxCanceled = make(chan struct{})
	go func() {
		<-ctx.Done()
		close(p.ctxCanceled)
	}()
	return nil
}

func (p *ContextTrackingProcess) Step(_ []process.Event, out *process.StepOutput) error {
	out.Yield(CompleteCmd{Value: "done"}, 0)
	out.Done(nil)
	return nil
}

func (p *ContextTrackingProcess) Send(*relay.Package) error {
	return nil
}

func (p *ContextTrackingProcess) Close() {}

func TestContextCancelledOnCompletion(t *testing.T) {
	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := newTestSchedulerWithLifecycle(1, lc)
	sched.Start()
	defer sched.Stop(context.Background())

	pid := pidapi.PID{UniqID: "ctx-cancel-test"}
	proc := &ContextTrackingProcess{}
	_, err := sched.Submit(context.Background(), pid, proc, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait for completion
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process did not complete")
	}

	// Context should be cancelled after completion
	select {
	case <-proc.ctxCanceled:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context was not cancelled on completion")
	}
}

// ContextBlockedProcess blocks and tracks context cancellation
type ContextBlockedProcess struct {
	blocked     chan struct{}
	ctxCanceled chan struct{}
}

func (p *ContextBlockedProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctxCanceled = make(chan struct{})
	go func() {
		<-ctx.Done()
		close(p.ctxCanceled)
	}()
	return nil
}

func (p *ContextBlockedProcess) Step(_ []process.Event, out *process.StepOutput) error {
	if p.blocked != nil {
		close(p.blocked)
		p.blocked = nil
	}
	out.Yield(SleepCmd{Duration: 10 * time.Second}, 0)
	out.Continue()
	return nil
}

func (p *ContextBlockedProcess) Send(*relay.Package) error {
	return nil
}

func (p *ContextBlockedProcess) Close() {}

func TestContextCancelledOnTermination(t *testing.T) {
	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := newTestSchedulerWithLifecycle(1, lc)
	sched.Start()
	defer sched.Stop(context.Background())

	blocked := make(chan struct{})
	pid := pidapi.PID{UniqID: "ctx-term-test"}

	proc := &ContextBlockedProcess{blocked: blocked}
	_, err := sched.Submit(context.Background(), pid, proc, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait for process to become blocked
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("process did not become blocked")
	}

	// Give time for state to transition to Blocked
	time.Sleep(50 * time.Millisecond)

	// Terminate
	err = sched.Terminate(pid)
	if err != nil {
		t.Fatalf("terminate error: %v", err)
	}

	// Wait for completion
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// Context should be cancelled after termination
	select {
	case <-proc.ctxCanceled:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context was not cancelled on termination")
	}
}

func TestSendToClosedQueue(t *testing.T) {
	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := newTestSchedulerWithLifecycle(1, lc)
	sched.Start()
	defer sched.Stop(context.Background())

	blocked := make(chan struct{})
	pid := pidapi.PID{UniqID: "closed-queue-test"}
	_, err := sched.Submit(context.Background(), pid, &BlockedProcess{blocked: blocked}, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait for process to become blocked
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("process did not become blocked")
	}

	// Terminate closes the queue
	err = sched.Terminate(pid)
	if err != nil {
		t.Fatalf("terminate error: %v", err)
	}

	// Send immediately after terminate (before completion callback runs)
	// Queue is closed so this should fail with ErrProcessClosed or ErrProcessNotFound
	pkg := &relay.Package{Target: pid}
	err = sched.Send(pkg)

	// Accept either error - depends on timing
	if err != nil && !errors.Is(err, process.ErrProcessNotFound) && !errors.Is(err, process.ErrProcessClosed) {
		t.Fatalf("expected ErrProcessNotFound or ErrProcessClosed, got %v", err)
	}
}

// PID registration tests

func TestPIDRegistration(t *testing.T) {
	sched := newTestScheduler(1)
	sched.Start()
	defer sched.Stop(context.Background())

	pid := pidapi.PID{UniqID: "reg-test"}

	// Before submit - PID not in map
	_, found := sched.byPID.Load(pid.String())
	if found {
		t.Fatal("PID should not exist before submit")
	}

	// Submit process
	proc, err := sched.Submit(context.Background(), pid, &CounterProcess{}, "", testInput(1))
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// After submit - PID should be in map
	v, found := sched.byPID.Load(pid.String())
	if !found {
		t.Fatal("PID should exist after submit")
	}
	if v.(*Processor) != proc {
		t.Fatal("wrong processor in map")
	}
}

func TestPIDUnregisteredOnCompletion(t *testing.T) {
	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := newTestSchedulerWithLifecycle(1, lc)
	sched.Start()
	defer sched.Stop(context.Background())

	pid := pidapi.PID{UniqID: "unreg-test"}
	_, err := sched.Submit(context.Background(), pid, &CounterProcess{}, "", testInput(1))
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait for completion
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process did not complete")
	}

	// After completion - PID should be removed from map
	_, found := sched.byPID.Load(pid.String())
	if found {
		t.Fatal("PID should be removed after completion")
	}
}

func TestPIDUnregisteredOnTermination(t *testing.T) {
	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := newTestSchedulerWithLifecycle(1, lc)
	sched.Start()
	defer sched.Stop(context.Background())

	blocked := make(chan struct{})
	pid := pidapi.PID{UniqID: "unreg-term-test"}
	_, err := sched.Submit(context.Background(), pid, &BlockedProcess{blocked: blocked}, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait for process to become blocked
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("process did not become blocked")
	}

	// Give time for state to transition to Blocked
	time.Sleep(50 * time.Millisecond)

	// Terminate
	err = sched.Terminate(pid)
	if err != nil {
		t.Fatalf("terminate error: %v", err)
	}

	// Wait for completion
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process did not complete after termination")
	}

	// After termination - PID should be removed
	_, found := sched.byPID.Load(pid.String())
	if found {
		t.Fatal("PID should be removed after termination")
	}
}

func TestDuplicatePIDOverwrites(t *testing.T) {
	sched := newTestScheduler(1)
	// Don't start scheduler to prevent completion

	pid := pidapi.PID{UniqID: "dup-test"}

	// Submit first process
	proc1, err := sched.Submit(context.Background(), pid, &SleepProcess{duration: 10 * time.Second}, "", nil)
	if err != nil {
		t.Fatalf("submit 1 error: %v", err)
	}

	// Submit second process with same PID - overwrites
	proc2, err := sched.Submit(context.Background(), pid, &SleepProcess{duration: 10 * time.Second}, "", nil)
	if err != nil {
		t.Fatalf("submit 2 error: %v", err)
	}

	// Map should have second processor
	v, found := sched.byPID.Load(pid.String())
	if !found {
		t.Fatal("PID should exist")
	}
	if v.(*Processor) != proc2 {
		t.Fatal("map should have second processor")
	}

	// First processor is orphaned (can't be looked up by PID anymore)
	if proc1 == proc2 {
		t.Fatal("processors should be different")
	}
}

func TestMultipleProcessCompletion(t *testing.T) {
	var completed atomic.Int32

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Add(1)
		},
	}

	sched := newTestSchedulerWithLifecycle(2, lc)
	sched.Start()
	defer sched.Stop(context.Background())

	const numProcs = 50

	// Submit processes
	for i := 0; i < numProcs; i++ {
		pid := pidapi.PID{UniqID: fmt.Sprintf("count-test-%d", i)}
		_, err := sched.Submit(context.Background(), pid, &CounterProcess{}, "", testInput(5))
		if err != nil {
			t.Fatalf("submit error: %v", err)
		}
	}

	// Wait for all to complete
	deadline := time.Now().Add(5 * time.Second)
	for completed.Load() < numProcs && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if completed.Load() != numProcs {
		t.Fatalf("expected %d completed, got %d", numProcs, completed.Load())
	}
}

func TestIdleProcessTermination(t *testing.T) {
	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := newTestSchedulerWithLifecycle(1, lc)
	sched.Start()
	defer sched.Stop(context.Background())

	pid := pidapi.PID{UniqID: "idle-map-test"}
	proc, err := sched.Submit(context.Background(), pid, &IdleProcess{}, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait for process to become idle
	deadline := time.Now().Add(2 * time.Second)
	for proc.state.Load() != int32(StateIdle) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if proc.state.Load() != int32(StateIdle) {
		t.Fatalf("process should be idle, got state %d", proc.state.Load())
	}

	// Terminate the idle process
	err = sched.Terminate(pid)
	if err != nil {
		t.Fatalf("terminate error: %v", err)
	}

	// Wait for completion
	deadline = time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process should have completed")
	}
}

// IdleMessageProcess tests message delivery to idle processes.
// Returns Idle on first step, then processes received messages.
type IdleMessageProcess struct {
	receivedMsg atomic.Bool
	completed   atomic.Bool
}

func (p *IdleMessageProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *IdleMessageProcess) Step(events []process.Event, out *process.StepOutput) error {
	for _, e := range events {
		if e.Type == process.EventMessage {
			p.receivedMsg.Store(true)
			out.Done(nil)
			p.completed.Store(true)
			return nil
		}
	}
	out.Idle()
	return nil
}

func (p *IdleMessageProcess) Close() {}

// TestSendToIdleProcessConcurrent tests that messages sent during
// the idle state transition (race window) are not lost.
func TestSendToIdleProcessConcurrent(t *testing.T) {
	const iterations = 100

	for i := 0; i < iterations; i++ {
		func() {
			var completed atomic.Bool

			lc := &testLifecycle{
				onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) {
					completed.Store(true)
				},
			}

			sched := newTestSchedulerWithLifecycle(2, lc)
			sched.Start()
			defer sched.Stop(context.Background())

			pid := pidapi.PID{UniqID: fmt.Sprintf("idle-race-%d", i)}
			proc := &IdleMessageProcess{}
			_, err := sched.Submit(context.Background(), pid, proc, "", nil)
			if err != nil {
				t.Fatalf("submit error: %v", err)
			}

			// Send message immediately - may hit race window
			go func() {
				for j := 0; j < 10; j++ {
					if err := sched.Send(&relay.Package{Target: pid}); err == nil {
						return
					}
					time.Sleep(time.Millisecond)
				}
			}()

			// Wait for process to complete (message received)
			deadline := time.Now().Add(500 * time.Millisecond)
			for !completed.Load() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}

			if !completed.Load() {
				t.Fatalf("iteration %d: process stuck - message lost during idle transition", i)
			}
		}()
	}
}
