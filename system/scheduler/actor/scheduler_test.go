package actor

import (
	"context"
	"fmt"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
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
	return dispatcher.HandlerFunc(func(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
		c := cmd.(CompleteCmd)
		emit.Emit(c.Value, nil)
		return nil
	})
}

func YieldHandler() dispatcher.Handler {
	return dispatcher.HandlerFunc(func(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
		emit.Emit(nil, nil)
		return nil
	})
}

func SleepHandler() dispatcher.Handler {
	return dispatcher.HandlerFunc(func(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
		s := cmd.(SleepCmd)
		timer := time.NewTimer(s.Duration)
		go func() {
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-timer.C:
				emit.Emit(nil, nil)
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

func (p *CounterProcess) Execute(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	if len(input) > 0 {
		p.target = input[0].Data().(int)
	}
	return nil
}

func (p *CounterProcess) Step(results *YieldResults) (StepResult, error) {
	if p.current >= p.target {
		var r StepResult
		r.Status = StepDone
		r.AddYield(CompleteCmd{Value: p.current})
		return r, nil
	}

	p.current++
	var r StepResult
	r.Status = StepContinue
	r.AddYield(YieldCmd{})
	return r, nil
}

func (p *CounterProcess) Send(pkg *relay.Package) error {
	return nil
}

func (p *CounterProcess) Close() {}

type SleepProcess struct {
	duration time.Duration
	slept    bool
	ctx      context.Context
}

func (p *SleepProcess) Execute(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	if len(input) > 0 {
		p.duration = input[0].Data().(time.Duration)
	}
	return nil
}

func (p *SleepProcess) Step(results *YieldResults) (StepResult, error) {
	if !p.slept {
		p.slept = true
		var r StepResult
		r.Status = StepContinue
		r.AddYield(SleepCmd{Duration: p.duration})
		return r, nil
	}

	var r StepResult
	r.Status = StepDone
	r.AddYield(CompleteCmd{Value: "done"})
	return r, nil
}

func (p *SleepProcess) Send(pkg *relay.Package) error {
	return nil
}

func (p *SleepProcess) Close() {}

// testLifecycle implements process2.Lifecycle for tests
type testLifecycle struct {
	onStart    func(context.Context, relay.PID, Process)
	onComplete func(context.Context, relay.PID, *runtime.Result)
}

func (l *testLifecycle) OnStart(ctx context.Context, pid relay.PID, proc Process) {
	if l.onStart != nil {
		l.onStart(ctx, pid, proc)
	}
}

func (l *testLifecycle) OnComplete(ctx context.Context, pid relay.PID, result *runtime.Result) {
	if l.onComplete != nil {
		l.onComplete(ctx, pid, result)
	}
}

// Helper to create test scheduler
func newTestScheduler(workers int) *Scheduler {
	registry := NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())
	registry.Register(CmdSleep, SleepHandler())
	return NewScheduler(registry, WithWorkers(workers))
}

func newTestSchedulerWithLifecycle(workers int, lc *testLifecycle) *Scheduler {
	registry := NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())
	registry.Register(CmdSleep, SleepHandler())
	return NewScheduler(registry, WithWorkers(workers), WithLifecycle(lc))
}

func testPID() relay.PID {
	return relay.PID{UniqID: "test"}
}

func testInput(v any) payload.Payloads {
	return payload.Payloads{payload.New(v)}
}

// Tests

func TestSchedulerBasic(t *testing.T) {
	var completed atomic.Bool
	var result *runtime.Result

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, res *runtime.Result) {
			result = res
			completed.Store(true)
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop()

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
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			if result.Error != nil {
				t.Errorf("process error: %v", result.Error)
				return
			}
			completedCount.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop()

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
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			if result.Error != nil {
				t.Errorf("error: %v", result.Error)
			}
			completed.Store(true)
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop()

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
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(4, lc)

	sched.Start()
	defer sched.Stop()

	for i := 0; i < 100; i++ {
		sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(50))
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

func TestSchedulerWakeTime(t *testing.T) {
	sched := newTestScheduler(1)

	// Submit before Start so we can safely read WakeNano
	before := nanotime()
	proc, err := sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(1))
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Read WakeNano before scheduler modifies it
	wakeTime := proc.WakeNano
	if wakeTime < before {
		t.Fatalf("wake time %d before submit time %d", wakeTime, before)
	}

	// Now start and let it complete
	sched.Start()
	defer sched.Stop()
}

func TestSchedulerStats(t *testing.T) {
	var completed atomic.Int32

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop()

	for i := 0; i < 10; i++ {
		sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(10))
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

// Execute tests

func TestSchedulerExecute(t *testing.T) {
	sched := newTestScheduler(2)
	sched.Start()
	defer sched.Stop()

	result, err := sched.Execute(context.Background(), testPID(), &CounterProcess{}, "", testInput(10))
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}
}

func TestSchedulerExecuteMultiple(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	var wg sync.WaitGroup
	errors := make([]error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result, err := sched.Execute(context.Background(), testPID(), &CounterProcess{}, "", testInput((idx+1)*10))
			if err != nil {
				errors[idx] = err
				return
			}
			if result.Error != nil {
				errors[idx] = result.Error
			}
		}(i)
	}

	wg.Wait()

	for i, err := range errors {
		if err != nil {
			t.Errorf("execute %d error: %v", i, err)
		}
	}
}

func TestSchedulerExecuteWithSleep(t *testing.T) {
	sched := newTestScheduler(2)
	sched.Start()
	defer sched.Stop()

	start := time.Now()
	result, err := sched.Execute(context.Background(), testPID(), &SleepProcess{}, "", testInput(50*time.Millisecond))
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
	var startPIDs []relay.PID
	var mu sync.Mutex

	lc := &testLifecycle{
		onStart: func(ctx context.Context, pid relay.PID, proc Process) {
			startCalls.Add(1)
			mu.Lock()
			startPIDs = append(startPIDs, pid)
			mu.Unlock()
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop()

	// Submit multiple processes
	for i := 0; i < 5; i++ {
		pid := relay.PID{UniqID: fmt.Sprintf("test-%d", i)}
		sched.Submit(context.Background(), pid, &CounterProcess{}, "", testInput(1))
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
	var completePIDs []relay.PID
	var completeResults []*runtime.Result
	var mu sync.Mutex

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completeCalls.Add(1)
			mu.Lock()
			completePIDs = append(completePIDs, pid)
			completeResults = append(completeResults, result)
			mu.Unlock()
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop()

	// Submit multiple processes
	for i := 0; i < 5; i++ {
		pid := relay.PID{UniqID: fmt.Sprintf("test-%d", i)}
		sched.Submit(context.Background(), pid, &CounterProcess{}, "", testInput(1))
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
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Store(true)
		},
	}
	sched := newTestSchedulerWithLifecycle(1, lc)

	// Don't start scheduler yet - this ensures process won't complete before Send()
	pid := relay.PID{UniqID: "send-test"}
	_, err := sched.Submit(context.Background(), pid, &SleepProcess{duration: 100 * time.Millisecond}, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Now start scheduler
	sched.Start()
	defer sched.Stop()

	// Send to active process should work
	pkg := &relay.Package{Target: pid}
	err = sched.Send(pid, pkg)
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}

	// Wait for completion via callback
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// After completion, Send should return ProcessNotFoundError
	err = sched.Send(pid, pkg)
	if err == nil {
		t.Fatal("expected error for completed PID")
	}
	if _, ok := err.(*ProcessNotFoundError); !ok {
		t.Fatalf("expected ProcessNotFoundError, got %T", err)
	}
}

// Allocation tests

func TestSchedulerSubmitAlloc(t *testing.T) {
	var completed atomic.Int32

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(1, lc)

	sched.Start()
	defer sched.Stop()

	pid := testPID()
	input := testInput(1)

	// Warm up
	for i := 0; i < 100; i++ {
		sched.Submit(context.Background(), pid, &CounterProcess{}, "", input)
	}

	deadline := time.Now().Add(5 * time.Second)
	for completed.Load() < 100 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// Measure allocations
	allocs := testing.AllocsPerRun(100, func() {
		sched.Submit(context.Background(), pid, &CounterProcess{}, "", input)
	})

	deadline = time.Now().Add(5 * time.Second)
	for completed.Load() < 200 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	t.Logf("Submit allocs per op: %f", allocs)
}

// Benchmarks

func BenchmarkSchedulerSubmit(b *testing.B) {
	var completed atomic.Int64

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(goruntime.GOMAXPROCS(0), lc)

	sched.Start()
	defer sched.Stop()

	ctx := context.Background()
	pid := testPID()
	input := testInput(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sched.Submit(ctx, pid, &CounterProcess{}, "", input)
	}

	for completed.Load() < int64(b.N) {
		goruntime.Gosched()
	}
}

func BenchmarkSchedulerThroughput(b *testing.B) {
	var completed atomic.Int64

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(goruntime.GOMAXPROCS(0), lc)

	sched.Start()
	defer sched.Stop()

	ctx := context.Background()
	pid := testPID()
	input := testInput(10)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sched.Submit(ctx, pid, &CounterProcess{}, "", input)
	}

	for completed.Load() < int64(b.N) {
		goruntime.Gosched()
	}
}

func BenchmarkSchedulerParallelSubmit(b *testing.B) {
	var completed atomic.Int64

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(goruntime.GOMAXPROCS(0), lc)

	sched.Start()
	defer sched.Stop()

	ctx := context.Background()
	pid := testPID()
	input := testInput(1)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sched.Submit(ctx, pid, &CounterProcess{}, "", input)
		}
	})

	for completed.Load() < int64(b.N) {
		goruntime.Gosched()
	}
}

func BenchmarkSchedulerExecute(b *testing.B) {
	sched := newTestScheduler(goruntime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	ctx := context.Background()
	pid := testPID()
	input := testInput(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sched.Execute(ctx, pid, &CounterProcess{}, "", input)
	}
}

func BenchmarkSchedulerParallelExecute(b *testing.B) {
	sched := newTestScheduler(goruntime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	ctx := context.Background()
	pid := testPID()
	input := testInput(1)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sched.Execute(ctx, pid, &CounterProcess{}, "", input)
		}
	})
}

// Memory leak tests

// TrackingProcess tracks Close() calls
type TrackingProcess struct {
	closeCalled *atomic.Int32
	ctx         context.Context
}

func (p *TrackingProcess) Execute(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	return nil
}

func (p *TrackingProcess) Step(results *YieldResults) (StepResult, error) {
	var r StepResult
	r.Status = StepDone
	r.AddYield(CompleteCmd{Value: "done"})
	return r, nil
}

func (p *TrackingProcess) Send(pkg *relay.Package) error {
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
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Add(1)
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop()

	const numProcs = 1000
	ctx := context.Background()

	for i := 0; i < numProcs; i++ {
		pid := relay.PID{Host: "test", UniqID: fmt.Sprintf("%d", i)}
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
	var byPIDCount, idleCount int
	sched.byPID.Range(func(k, v any) bool {
		byPIDCount++
		return true
	})
	sched.idle.Range(func(k, v any) bool {
		idleCount++
		return true
	})

	if byPIDCount != 0 {
		t.Errorf("byPID map has %d entries, expected 0", byPIDCount)
	}
	if idleCount != 0 {
		t.Errorf("idle map has %d entries, expected 0", idleCount)
	}
}
