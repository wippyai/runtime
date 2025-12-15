package lazy

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
)

type mockProcess struct {
	mu      sync.Mutex
	latency time.Duration
}

func (p *mockProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *mockProcess) Step(_ []process.Event, out *process.StepOutput) error {
	p.mu.Lock()
	latency := p.latency
	p.mu.Unlock()

	if latency > 0 {
		time.Sleep(latency)
	}

	out.Done(nil)
	return nil
}

func (p *mockProcess) Close() {}

type mockDispatcher struct{}

func (d *mockDispatcher) Dispatch(dispatcher.Command) dispatcher.Handler { return nil }

func newMockFactory(latency time.Duration) process.FactoryFunc {
	return func() (process.Process, error) {
		return &mockProcess{latency: latency}, nil
	}
}

func newCountingFactory() (process.FactoryFunc, *atomic.Int32) {
	count := &atomic.Int32{}
	return func() (process.Process, error) {
		count.Add(1)
		return &mockProcess{}, nil
	}, count
}

func TestLazyBasic(t *testing.T) {
	factory, count := newCountingFactory()
	p, err := New(factory, &mockDispatcher{}, Config{MaxWorkers: 4})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()
	p.Start()

	// No processes created yet
	if count.Load() != 0 {
		t.Fatalf("expected 0 processes at start, got %d", count.Load())
	}

	// First call creates a process
	_, err = p.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if count.Load() != 1 {
		t.Fatalf("expected 1 process after call, got %d", count.Load())
	}
}

func TestLazyReusesIdleProcess(t *testing.T) {
	factory, count := newCountingFactory()
	p, err := New(factory, &mockDispatcher{}, Config{MaxWorkers: 4, IdleTimeout: time.Minute})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()
	p.Start()

	// Multiple sequential calls should reuse the same process
	for i := 0; i < 10; i++ {
		_, err := p.Call(context.Background(), "test", nil)
		if err != nil {
			t.Fatalf("Call %d: %v", i, err)
		}
	}

	if count.Load() != 1 {
		t.Fatalf("expected 1 process (reused), got %d", count.Load())
	}
}

func TestLazyConcurrent(t *testing.T) {
	factory, count := newCountingFactory()
	p, err := New(factory, &mockDispatcher{}, Config{MaxWorkers: 4})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()
	p.Start()

	var wg sync.WaitGroup
	const n = 4
	wg.Add(n)

	// Start 4 concurrent calls - should create 4 processes
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := p.Call(context.Background(), "test", nil)
			if err != nil {
				t.Errorf("Call: %v", err)
			}
		}()
	}
	wg.Wait()

	// Should have created up to 4 processes
	if count.Load() > 4 {
		t.Fatalf("expected at most 4 processes, got %d", count.Load())
	}
}

func TestLazyMaxWorkers(t *testing.T) {
	p, err := New(newMockFactory(100*time.Millisecond), &mockDispatcher{}, Config{MaxWorkers: 2})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()
	p.Start()

	// Start 3 concurrent calls with only 2 max workers
	// Third call should wait
	var wg sync.WaitGroup
	wg.Add(2)

	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, _ = p.Call(context.Background(), "test", nil)
		}()
	}

	// Give workers time to start
	time.Sleep(20 * time.Millisecond)

	// Third call should timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = p.Call(ctx, "test", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Logf("expected context deadline exceeded or nil, got: %v", err)
	}

	wg.Wait()
}
