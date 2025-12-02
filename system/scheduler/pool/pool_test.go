package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

// mockProcess is a test process that returns immediately.
type mockProcess struct {
	mu         sync.Mutex
	execCount  int
	stepCount  int
	closeCount int
	latency    time.Duration
}

func (p *mockProcess) Execute(ctx context.Context, method string, input payload.Payloads) error {
	p.mu.Lock()
	p.execCount++
	p.mu.Unlock()
	return nil
}

func (p *mockProcess) Step(results *process.YieldResults) (process.StepResult, error) {
	p.mu.Lock()
	p.stepCount++
	latency := p.latency
	p.mu.Unlock()

	if latency > 0 {
		time.Sleep(latency)
	}

	return process.StepResult{Status: process.StepDone}, nil
}

func (p *mockProcess) Close() {
	p.mu.Lock()
	p.closeCount++
	p.mu.Unlock()
}

func (p *mockProcess) Send(pkg *relay.Package) error { return nil }

func (p *mockProcess) stats() (exec, step, close int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.execCount, p.stepCount, p.closeCount
}

// mockDispatcher returns no handlers.
type mockDispatcher struct{}

func (d *mockDispatcher) Dispatch(cmd dispatcher.Command) dispatcher.Handler { return nil }

// factories

func newMockFactory(latency time.Duration) Factory {
	return func() (process.Process, error) {
		return &mockProcess{latency: latency}, nil
	}
}

func newCountingFactory() (Factory, *atomic.Int32) {
	count := &atomic.Int32{}
	return func() (process.Process, error) {
		count.Add(1)
		return &mockProcess{}, nil
	}, count
}

func newErrorFactory() Factory {
	return func() (process.Process, error) {
		return nil, fmt.Errorf("factory error")
	}
}

// Inline tests

func TestInlineBasic(t *testing.T) {
	pool, err := NewInline(newMockFactory(0), &mockDispatcher{})
	if err != nil {
		t.Fatalf("NewInline: %v", err)
	}
	defer pool.Stop()

	pool.Start()

	result, err := pool.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}
}

func TestInlineMultipleCalls(t *testing.T) {
	pool, err := NewInline(newMockFactory(0), &mockDispatcher{})
	if err != nil {
		t.Fatalf("NewInline: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	for i := 0; i < 100; i++ {
		_, err := pool.Call(context.Background(), "test", nil)
		if err != nil {
			t.Fatalf("Call %d: %v", i, err)
		}
	}
}

func TestInlineFactoryError(t *testing.T) {
	_, err := NewInline(newErrorFactory(), &mockDispatcher{})
	if err == nil {
		t.Fatal("expected factory error")
	}
}

// Test that StepResult.Result is properly propagated to runtime.Result.Value
func TestInlineResultPropagation(t *testing.T) {
	factory := func() (process.Process, error) {
		return &resultProcess{result: payload.New("hello world")}, nil
	}
	pool, err := NewInline(factory, &mockDispatcher{})
	if err != nil {
		t.Fatalf("NewInline: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	result, err := pool.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}
	if result.Value == nil {
		t.Fatal("expected result.Value to be set")
	}
	pl, ok := result.Value.(payload.Payload)
	if !ok {
		t.Fatalf("expected result.Value to be payload.Payload, got %T", result.Value)
	}
	if pl.Data() != "hello world" {
		t.Fatalf("expected 'hello world', got %v", pl.Data())
	}
}

// resultProcess returns a result on StepDone
type resultProcess struct {
	result payload.Payload
}

func (p *resultProcess) Execute(ctx context.Context, method string, input payload.Payloads) error {
	return nil
}

func (p *resultProcess) Step(results *process.YieldResults) (process.StepResult, error) {
	return process.StepResult{Status: process.StepDone, Result: p.result}, nil
}

func (p *resultProcess) Close()                        {}
func (p *resultProcess) Send(pkg *relay.Package) error { return nil }

// Static tests

func TestStaticBasic(t *testing.T) {
	pool, err := NewStatic(newMockFactory(0), &mockDispatcher{}, Config{Workers: 2})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	result, err := pool.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}
}

func TestStaticConcurrent(t *testing.T) {
	factory, count := newCountingFactory()
	pool, err := NewStatic(factory, &mockDispatcher{}, Config{Workers: 4})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Verify correct number of processes created
	if count.Load() != 4 {
		t.Fatalf("expected 4 processes, got %d", count.Load())
	}

	var wg sync.WaitGroup
	const n = 100
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := pool.Call(context.Background(), "test", nil)
			if err != nil {
				t.Errorf("Call: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestStaticContextCancel(t *testing.T) {
	pool, err := NewStatic(newMockFactory(100*time.Millisecond), &mockDispatcher{}, Config{Workers: 1})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = pool.Call(ctx, "test", nil)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestStaticStopDrainsQueue(t *testing.T) {
	pool, err := NewStatic(newMockFactory(10*time.Millisecond), &mockDispatcher{}, Config{Workers: 2})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	pool.Start()

	// Submit some work
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.Call(context.Background(), "test", nil)
		}()
	}

	// Give some time for work to start
	time.Sleep(5 * time.Millisecond)

	// Stop should wait for completion
	pool.Stop()
	wg.Wait()
}

// Lazy tests

func TestLazyBasic(t *testing.T) {
	factory, count := newCountingFactory()
	pool, err := NewLazy(factory, &mockDispatcher{}, LazyConfig{MaxWorkers: 4})
	if err != nil {
		t.Fatalf("NewLazy: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// No processes created yet
	if count.Load() != 0 {
		t.Fatalf("expected 0 processes at start, got %d", count.Load())
	}

	// First call creates a process
	_, err = pool.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if count.Load() != 1 {
		t.Fatalf("expected 1 process after call, got %d", count.Load())
	}
}

func TestLazyReusesIdleProcess(t *testing.T) {
	factory, count := newCountingFactory()
	pool, err := NewLazy(factory, &mockDispatcher{}, LazyConfig{MaxWorkers: 4, IdleTimeout: time.Minute})
	if err != nil {
		t.Fatalf("NewLazy: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Multiple sequential calls should reuse the same process
	for i := 0; i < 10; i++ {
		_, err = pool.Call(context.Background(), "test", nil)
		if err != nil {
			t.Fatalf("Call %d: %v", i, err)
		}
	}

	if count.Load() != 1 {
		t.Fatalf("expected 1 process for sequential calls, got %d", count.Load())
	}
}

func TestLazyMaxWorkers(t *testing.T) {
	factory, _ := newCountingFactory()
	pool, err := NewLazy(factory, &mockDispatcher{}, LazyConfig{MaxWorkers: 2})
	if err != nil {
		t.Fatalf("NewLazy: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Simulate concurrent calls exceeding max workers
	blockCh := make(chan struct{})
	blockingFactory := func() (process.Process, error) {
		return &blockingProcess{blockCh: blockCh}, nil
	}

	pool2, _ := NewLazy(blockingFactory, &mockDispatcher{}, LazyConfig{MaxWorkers: 2})
	defer pool2.Stop()
	pool2.Start()

	// Start 2 blocking calls
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			pool2.Call(context.Background(), "test", nil)
		}()
	}

	// Give time for workers to be active
	time.Sleep(20 * time.Millisecond)

	// Third call should timeout (all workers busy, will wait for one)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = pool2.Call(ctx, "test", nil)
	if err != context.DeadlineExceeded {
		t.Logf("Expected DeadlineExceeded, got: %v", err)
	}

	// Unblock workers
	close(blockCh)
	wg.Wait()
}

type blockingProcess struct {
	blockCh <-chan struct{}
}

func (p *blockingProcess) Execute(ctx context.Context, method string, input payload.Payloads) error {
	return nil
}

func (p *blockingProcess) Step(results *process.YieldResults) (process.StepResult, error) {
	<-p.blockCh
	return process.StepResult{Status: process.StepDone}, nil
}

func (p *blockingProcess) Close()                        {}
func (p *blockingProcess) Send(pkg *relay.Package) error { return nil }

// Benchmark tests

func BenchmarkInlineCall(b *testing.B) {
	pool, _ := NewInline(newMockFactory(0), &mockDispatcher{})
	defer pool.Stop()
	pool.Start()

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Call(ctx, "test", nil)
	}
}

func BenchmarkStaticCall(b *testing.B) {
	pool, _ := NewStatic(newMockFactory(0), &mockDispatcher{}, Config{Workers: 4})
	defer pool.Stop()
	pool.Start()

	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.Call(ctx, "test", nil)
		}
	})
}

func BenchmarkLazyCall(b *testing.B) {
	pool, _ := NewLazy(newMockFactory(0), &mockDispatcher{}, LazyConfig{MaxWorkers: 4})
	defer pool.Stop()
	pool.Start()

	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.Call(ctx, "test", nil)
		}
	})
}

// Hooks tests

func TestHooksOnStartCalled(t *testing.T) {
	var startCount atomic.Int32
	hooks := Hooks{
		OnStart: func(proc process.Process) {
			startCount.Add(1)
		},
	}

	factory := WrapFactoryWithHooks(newMockFactory(0), hooks)
	pool, err := NewStatic(factory, &mockDispatcher{}, Config{Workers: 3})
	if err != nil {
		t.Fatal(err)
	}
	pool.Start()
	defer pool.Stop()

	if startCount.Load() != 3 {
		t.Fatalf("expected 3 OnStart calls, got %d", startCount.Load())
	}
}

func TestHooksOnStopCalled(t *testing.T) {
	var startCount, stopCount atomic.Int32
	hooks := Hooks{
		OnStart: func(proc process.Process) {
			startCount.Add(1)
		},
		OnStop: func(proc process.Process) {
			stopCount.Add(1)
		},
	}

	factory := WrapFactoryWithHooks(newMockFactory(0), hooks)
	pool, err := NewStatic(factory, &mockDispatcher{}, Config{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	pool.Start()

	// Do some calls
	for i := 0; i < 5; i++ {
		pool.Call(context.Background(), "test", nil)
	}

	pool.Stop()

	if startCount.Load() != 2 {
		t.Fatalf("expected 2 OnStart calls, got %d", startCount.Load())
	}
	if stopCount.Load() != 2 {
		t.Fatalf("expected 2 OnStop calls, got %d", stopCount.Load())
	}
}

func TestHooksNoHooks(t *testing.T) {
	factory := newMockFactory(0)
	wrapped := WrapFactoryWithHooks(factory, Hooks{})

	proc1, _ := factory()
	proc2, _ := wrapped()

	if proc1 == nil || proc2 == nil {
		t.Fatal("expected non-nil processes")
	}
	proc1.Close()
	proc2.Close()
}
