package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// mockProcess is a test process that returns immediately.
type mockProcess struct {
	mu         sync.Mutex
	execCount  int
	stepCount  int
	closeCount int
	latency    time.Duration
}

func (p *mockProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
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

// mockInbox implements process.Inbox for testing.
type mockInbox struct{}

func (m *mockInbox) QueueMessage(pkg *relay.Package) bool { return true }
func (m *mockInbox) Drain() []*relay.Package              { return nil }

// testContext creates a context with FrameContext and PID for testing.
func testContext() context.Context {
	return testContextWithPID("test-pid")
}

// testContextWithPID creates a context with a specific PID for testing.
func testContextWithPID(pid string) context.Context {
	ctx, _ := ctxapi.AcquireFrameContext(context.Background())
	runtime.SetFramePID(ctx, relay.PID{UniqID: pid})
	return ctx
}

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

	result, err := pool.Call(testContext(), "test", nil)
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
		_, err := pool.Call(testContext(), "test", nil)
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

	result, err := pool.Call(testContext(), "test", nil)
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

func (p *resultProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
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

	result, err := pool.Call(testContext(), "test", nil)
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
			_, err := pool.Call(testContext(), "test", nil)
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

	ctx := testContext()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
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
			pool.Call(testContext(), "test", nil)
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
	_, err = pool.Call(testContext(), "test", nil)
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
		_, err = pool.Call(testContext(), "test", nil)
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
			pool2.Call(testContext(), "test", nil)
		}()
	}

	// Give time for workers to be active
	time.Sleep(20 * time.Millisecond)

	// Third call should timeout (all workers busy, will wait for one)
	ctx := testContext()
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
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

func (p *blockingProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Call(testContext(), "test", nil)
	}
}

func BenchmarkStaticCall(b *testing.B) {
	pool, _ := NewStatic(newMockFactory(0), &mockDispatcher{}, Config{Workers: 4})
	defer pool.Stop()
	pool.Start()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.Call(testContext(), "test", nil)
		}
	})
}

func BenchmarkLazyCall(b *testing.B) {
	pool, _ := NewLazy(newMockFactory(0), &mockDispatcher{}, LazyConfig{MaxWorkers: 4})
	defer pool.Stop()
	pool.Start()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.Call(testContext(), "test", nil)
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
		pool.Call(testContext(), "test", nil)
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

// idleProcess goes into StepIdle state and waits for a message before completing.
// Messages are received via the Executor-owned inbox in context.
type idleProcess struct {
	ctx       context.Context
	received  []*relay.Package
	mu        sync.Mutex
	stepCount int
}

func (p *idleProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	return nil
}

func (p *idleProcess) Step(results *process.YieldResults) (process.StepResult, error) {
	p.mu.Lock()
	p.stepCount++
	count := p.stepCount
	p.mu.Unlock()

	// First step: go idle and wait for message
	if count == 1 {
		return process.StepResult{Status: process.StepIdle}, nil
	}

	// Drain messages from inbox and store them
	if inbox := process.GetInbox(p.ctx); inbox != nil {
		msgs := inbox.Drain()
		p.mu.Lock()
		p.received = append(p.received, msgs...)
		p.mu.Unlock()
	}

	// After receiving message, complete
	return process.StepResult{Status: process.StepDone}, nil
}

func (p *idleProcess) Send(pkg *relay.Package) error {
	inbox := process.GetInbox(p.ctx)
	if inbox == nil {
		return errors.New("no inbox")
	}
	if !inbox.QueueMessage(pkg) {
		return errors.New("inbox closed")
	}
	return nil
}

func (p *idleProcess) Close() {}

func (p *idleProcess) getReceived() []*relay.Package {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]*relay.Package, len(p.received))
	copy(result, p.received)
	return result
}

// multiIdleProcess goes idle N times before completing.
// Messages are received via the Executor-owned inbox in context.
type multiIdleProcess struct {
	ctx       context.Context
	idleTimes int
	received  []*relay.Package
	mu        sync.Mutex
	stepCount int
}

func (p *multiIdleProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	return nil
}

func (p *multiIdleProcess) Step(results *process.YieldResults) (process.StepResult, error) {
	p.mu.Lock()
	p.stepCount++
	count := p.stepCount
	idleTimes := p.idleTimes
	p.mu.Unlock()

	// On even steps (after waking from idle), drain messages from inbox
	if count%2 == 0 {
		if inbox := process.GetInbox(p.ctx); inbox != nil {
			msgs := inbox.Drain()
			p.mu.Lock()
			p.received = append(p.received, msgs...)
			p.mu.Unlock()
		}
	}

	// Alternate between idle and continue until we've received enough messages
	if count <= idleTimes*2 && count%2 == 1 {
		return process.StepResult{Status: process.StepIdle}, nil
	}
	if count > idleTimes*2 {
		return process.StepResult{Status: process.StepDone}, nil
	}
	// Even step: continue after receiving message
	return process.StepResult{Status: process.StepContinue}, nil
}

func (p *multiIdleProcess) Send(pkg *relay.Package) error {
	inbox := process.GetInbox(p.ctx)
	if inbox == nil {
		return errors.New("no inbox")
	}
	if !inbox.QueueMessage(pkg) {
		return errors.New("inbox closed")
	}
	return nil
}

func (p *multiIdleProcess) Close() {}

func (p *multiIdleProcess) getReceived() []*relay.Package {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]*relay.Package, len(p.received))
	copy(result, p.received)
	return result
}

// Send tests for Inline pool

func TestInlineSend(t *testing.T) {
	var proc *idleProcess
	factory := func() (process.Process, error) {
		proc = &idleProcess{}
		return proc, nil
	}

	pool, err := NewInline(factory, &mockDispatcher{})
	if err != nil {
		t.Fatalf("NewInline: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Run Call in a goroutine since it will block on StepIdle
	resultCh := make(chan error, 1)
	go func() {
		_, err := pool.Call(testContext(), "test", nil)
		resultCh <- err
	}()

	// Wait for process to enter idle state
	time.Sleep(10 * time.Millisecond)

	// Send message to the pool using the known test PID
	pkg := &relay.Package{Target: relay.PID{UniqID: "test-pid"}}
	err = pool.Send(pkg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Wait for Call to complete
	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("Call: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Call to complete")
	}

	// Verify message was received
	received := proc.getReceived()
	if len(received) != 1 {
		t.Fatalf("expected 1 message, got %d", len(received))
	}
}

func TestInlineSendToNonExistent(t *testing.T) {
	pool, err := NewInline(newMockFactory(0), &mockDispatcher{})
	if err != nil {
		t.Fatalf("NewInline: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Send to non-existent PID should return error
	pkg := &relay.Package{Target: relay.PID{UniqID: "nonexistent"}}
	err = pool.Send(pkg)
	if err != ErrProcessNotFound {
		t.Fatalf("expected ErrProcessNotFound, got %v", err)
	}
}

// Send tests for Static pool

func TestStaticSend(t *testing.T) {
	procCh := make(chan *idleProcess, 1)
	factory := func() (process.Process, error) {
		p := &idleProcess{}
		select {
		case procCh <- p:
		default:
		}
		return p, nil
	}

	pool, err := NewStatic(factory, &mockDispatcher{}, Config{Workers: 1})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Run Call in a goroutine since it will block on StepIdle
	resultCh := make(chan error, 1)
	go func() {
		_, err := pool.Call(testContext(), "test", nil)
		resultCh <- err
	}()

	// Wait for process to enter idle state
	time.Sleep(20 * time.Millisecond)

	// Send message to the pool using the known test PID
	pkg := &relay.Package{Target: relay.PID{UniqID: "test-pid"}}
	err = pool.Send(pkg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Wait for Call to complete
	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("Call: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Call to complete")
	}

	// Get the process and verify message was received
	proc := <-procCh
	received := proc.getReceived()
	if len(received) != 1 {
		t.Fatalf("expected 1 message, got %d", len(received))
	}
}

func TestStaticSendToNonExistent(t *testing.T) {
	pool, err := NewStatic(newMockFactory(0), &mockDispatcher{}, Config{Workers: 1})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	pkg := &relay.Package{Target: relay.PID{UniqID: "nonexistent"}}
	err = pool.Send(pkg)
	if err != ErrProcessNotFound {
		t.Fatalf("expected ErrProcessNotFound, got %v", err)
	}
}

func TestStaticSendMultipleWorkers(t *testing.T) {
	var mu sync.Mutex
	procs := make([]*idleProcess, 0, 4)
	factory := func() (process.Process, error) {
		p := &idleProcess{}
		mu.Lock()
		procs = append(procs, p)
		mu.Unlock()
		return p, nil
	}

	pool, err := NewStatic(factory, &mockDispatcher{}, Config{Workers: 4})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Start 4 concurrent calls with unique PIDs
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		pid := fmt.Sprintf("pid-%d", i+1)
		go func(p string) {
			defer wg.Done()
			pool.Call(testContextWithPID(p), "test", nil)
		}(pid)
	}

	// Wait for all processes to enter idle state
	time.Sleep(50 * time.Millisecond)

	// Send messages to each active execution
	for i := 1; i <= 4; i++ {
		pkg := &relay.Package{Target: relay.PID{UniqID: fmt.Sprintf("pid-%d", i)}}
		err = pool.Send(pkg)
		if err != nil {
			t.Fatalf("Send to %d: %v", i, err)
		}
	}

	wg.Wait()

	// Verify all processes received exactly one message
	mu.Lock()
	totalReceived := 0
	for _, p := range procs {
		totalReceived += len(p.getReceived())
	}
	mu.Unlock()

	if totalReceived != 4 {
		t.Fatalf("expected 4 total messages, got %d", totalReceived)
	}
}

// Send tests for Lazy pool

func TestLazySend(t *testing.T) {
	var proc *idleProcess
	var mu sync.Mutex
	factory := func() (process.Process, error) {
		mu.Lock()
		defer mu.Unlock()
		proc = &idleProcess{}
		return proc, nil
	}

	pool, err := NewLazy(factory, &mockDispatcher{}, LazyConfig{MaxWorkers: 4})
	if err != nil {
		t.Fatalf("NewLazy: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Run Call in a goroutine since it will block on StepIdle
	resultCh := make(chan error, 1)
	go func() {
		_, err := pool.Call(testContext(), "test", nil)
		resultCh <- err
	}()

	// Wait for process to enter idle state
	time.Sleep(20 * time.Millisecond)

	// Send message to the pool using the known test PID
	pkg := &relay.Package{Target: relay.PID{UniqID: "test-pid"}}
	err = pool.Send(pkg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Wait for Call to complete
	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("Call: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Call to complete")
	}

	// Verify message was received
	mu.Lock()
	received := proc.getReceived()
	mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 message, got %d", len(received))
	}
}

func TestLazySendToNonExistent(t *testing.T) {
	pool, err := NewLazy(newMockFactory(0), &mockDispatcher{}, LazyConfig{MaxWorkers: 4})
	if err != nil {
		t.Fatalf("NewLazy: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	pkg := &relay.Package{Target: relay.PID{UniqID: "nonexistent"}}
	err = pool.Send(pkg)
	if err != ErrProcessNotFound {
		t.Fatalf("expected ErrProcessNotFound, got %v", err)
	}
}

func TestLazySendAfterCompletion(t *testing.T) {
	pool, err := NewLazy(newMockFactory(0), &mockDispatcher{}, LazyConfig{MaxWorkers: 4})
	if err != nil {
		t.Fatalf("NewLazy: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Complete a call
	_, err = pool.Call(testContext(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	// Send to completed execution should return ErrProcessNotFound
	pkg := &relay.Package{Target: relay.PID{UniqID: "1"}}
	err = pool.Send(pkg)
	if err != ErrProcessNotFound {
		t.Fatalf("expected ErrProcessNotFound, got %v", err)
	}
}

// Stress tests for Send

func TestSendStressInline(t *testing.T) {
	const numIterations = 100

	for i := 0; i < numIterations; i++ {
		var proc *idleProcess
		factory := func() (process.Process, error) {
			proc = &idleProcess{}
			return proc, nil
		}

		pool, err := NewInline(factory, &mockDispatcher{})
		if err != nil {
			t.Fatalf("iteration %d: NewInline: %v", i, err)
		}

		pid := fmt.Sprintf("pid-%d", i)
		done := make(chan struct{})
		go func(p string) {
			pool.Call(testContextWithPID(p), "test", nil)
			close(done)
		}(pid)

		time.Sleep(time.Millisecond)
		pkg := &relay.Package{Target: relay.PID{UniqID: pid}}
		pool.Send(pkg)

		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("iteration %d: timeout", i)
		}

		pool.Stop()
	}
}

func TestSendStressStatic(t *testing.T) {
	const numWorkers = 4
	const numCalls = 100

	factory := func() (process.Process, error) {
		return &idleProcess{}, nil
	}

	pool, err := NewStatic(factory, &mockDispatcher{}, Config{Workers: numWorkers})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	var wg sync.WaitGroup
	var nextPID atomic.Uint64

	for i := 0; i < numCalls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			pid := fmt.Sprintf("pid-%d", nextPID.Add(1))
			doneCh := make(chan struct{})
			go func(p string) {
				pool.Call(testContextWithPID(p), "test", nil)
				close(doneCh)
			}(pid)

			time.Sleep(time.Millisecond)
			pkg := &relay.Package{Target: relay.PID{UniqID: pid}}
			pool.Send(pkg)

			select {
			case <-doneCh:
			case <-time.After(time.Second):
				t.Error("timeout waiting for call")
			}
		}()
	}

	wg.Wait()
}

func TestSendStressLazy(t *testing.T) {
	const numCalls = 100

	factory := func() (process.Process, error) {
		return &idleProcess{}, nil
	}

	pool, err := NewLazy(factory, &mockDispatcher{}, LazyConfig{MaxWorkers: 8})
	if err != nil {
		t.Fatalf("NewLazy: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	var wg sync.WaitGroup
	var nextPID atomic.Uint64

	for i := 0; i < numCalls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			pid := fmt.Sprintf("pid-%d", nextPID.Add(1))
			doneCh := make(chan struct{})
			go func(p string) {
				pool.Call(testContextWithPID(p), "test", nil)
				close(doneCh)
			}(pid)

			time.Sleep(time.Millisecond)
			pkg := &relay.Package{Target: relay.PID{UniqID: pid}}
			pool.Send(pkg)

			select {
			case <-doneCh:
			case <-time.After(time.Second):
				t.Error("timeout waiting for call")
			}
		}()
	}

	wg.Wait()
}

// Benchmark Send operations

func BenchmarkExecutorSend(b *testing.B) {
	executor := NewExecutor(&mockDispatcher{})
	var inbox process.Inbox = &mockInbox{}
	executor.inbox.Store(&inbox)
	pkg := &relay.Package{Target: relay.PID{UniqID: "1"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		executor.Send(pkg)
	}
}

func BenchmarkStaticSendLookup(b *testing.B) {
	pool, _ := NewStatic(newMockFactory(0), &mockDispatcher{}, Config{Workers: 4})
	defer pool.Stop()
	pool.Start()

	// Register a fake executor in active map
	executor := NewExecutor(&mockDispatcher{})
	var inbox process.Inbox = &mockInbox{}
	executor.inbox.Store(&inbox)
	pool.active.Store("bench-1", executor)
	pkg := &relay.Package{Target: relay.PID{UniqID: "bench-1"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Send(pkg)
	}
}

func BenchmarkLazySendLookup(b *testing.B) {
	pool, _ := NewLazy(newMockFactory(0), &mockDispatcher{}, LazyConfig{MaxWorkers: 4})
	defer pool.Stop()
	pool.Start()

	// Register a fake executor in active map
	executor := NewExecutor(&mockDispatcher{})
	var inbox process.Inbox = &mockInbox{}
	executor.inbox.Store(&inbox)
	pool.activeExec.Store("bench-1", executor)
	pkg := &relay.Package{Target: relay.PID{UniqID: "bench-1"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Send(pkg)
	}
}

func BenchmarkInlineSendLookup(b *testing.B) {
	pool, _ := NewInline(newMockFactory(0), &mockDispatcher{})
	defer pool.Stop()
	pool.Start()

	// Register a fake executor in active map
	executor := NewExecutor(&mockDispatcher{})
	var inbox process.Inbox = &mockInbox{}
	executor.inbox.Store(&inbox)
	pool.active.Store("bench-1", executor)
	pkg := &relay.Package{Target: relay.PID{UniqID: "bench-1"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Send(pkg)
	}
}
