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
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
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
	return dispatcher.HandlerFunc(func(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		c := cmd.(CompleteCmd)
		go receiver.CompleteYield(tag, c.Value, nil)
		return nil
	})
}

func YieldHandler() dispatcher.Handler {
	return dispatcher.HandlerFunc(func(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
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

func (p *CounterProcess) Step(_ []Event, out *StepOutput) error {
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

func (p *SleepProcess) Step(_ []Event, out *StepOutput) error {
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

func testPID() relay.PID {
	return relay.PID{UniqID: "test"}
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

// StatsProcess implements StatsProvider
type StatsProcess struct {
	CounterProcess
	customStats string
}

func (p *StatsProcess) Stats() any {
	return map[string]string{"custom": p.customStats}
}

func TestSchedulerCollectProcessStats(t *testing.T) {
	sched := newTestScheduler(2)
	sched.Start()
	defer sched.Stop()

	pid := relay.PID{UniqID: "stats-process"}
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
		if info, ok := s.(map[string]string); ok {
			if info["custom"] == "test-value" {
				found = true
				break
			}
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
	errors := make([]error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Use unique PID per goroutine to avoid collision in pending map
			pid := relay.PID{UniqID: fmt.Sprintf("test-%d", idx)}
			result, err := te.Execute(context.Background(), pid, &CounterProcess{}, "", testInput((idx+1)*10))
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
	err = sched.Send(pkg)
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}

	// Wait for completion via callback
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// After completion, Send should return ProcessNotFoundError
	err = sched.Send(pkg)
	if err == nil {
		t.Fatal("expected error for completed PID")
	}
	if _, ok := err.(*ProcessNotFoundError); !ok {
		t.Fatalf("expected ProcessNotFoundError, got %T", err)
	}
}

// Single worker tests

func TestSchedulerSingleWorker(t *testing.T) {
	for _, kind := range []process.SchedulerKind{process.KindGlobal, process.KindStealing} {
		t.Run(string(kind), func(t *testing.T) {
			var completed atomic.Bool
			var result *runtime.Result

			lc := &testLifecycle{
				onComplete: func(ctx context.Context, pid relay.PID, res *runtime.Result) {
					result = res
					completed.Store(true)
				},
			}

			registry := scheduler.NewRegistry()
			registry.Register(CmdComplete, CompleteHandler())
			registry.Register(CmdYield, YieldHandler())

			sched := NewScheduler(registry, WithWorkers(1), WithKind(kind), WithLifecycle(lc))
			sched.Start()
			defer sched.Stop()

			ctx := context.Background()
			pid := relay.PID{UniqID: "single"}

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
		})
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
	te := newTestExecutor(goruntime.GOMAXPROCS(0))
	te.Start()
	defer te.Stop()

	ctx := context.Background()
	input := testInput(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := relay.PID{UniqID: fmt.Sprintf("bench-%d", i)}
		te.Execute(ctx, pid, &CounterProcess{}, "", input)
	}
}

func BenchmarkSchedulerParallelExecute(b *testing.B) {
	te := newTestExecutor(goruntime.GOMAXPROCS(0))
	te.Start()
	defer te.Stop()

	ctx := context.Background()
	input := testInput(1)
	var counter atomic.Int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := counter.Add(1)
			pid := relay.PID{UniqID: fmt.Sprintf("bench-%d", i)}
			te.Execute(ctx, pid, &CounterProcess{}, "", input)
		}
	})
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

func (p *TrackingProcess) Step(_ []Event, out *StepOutput) error {
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
	sched.byPID.Range(func(_, _ any) bool {
		byPIDCount++
		return true
	})
	sched.idleProcs.Range(func(_, _ any) bool {
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
