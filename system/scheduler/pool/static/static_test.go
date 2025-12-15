package static

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

func TestStaticBasic(t *testing.T) {
	p, err := New(newMockFactory(0), &mockDispatcher{}, Config{Workers: 2})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()
	p.Start()

	result, err := p.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}
}

func TestStaticConcurrent(t *testing.T) {
	factory, count := newCountingFactory()
	p, err := New(factory, &mockDispatcher{}, Config{Workers: 4})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()
	p.Start()

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
			_, err := p.Call(context.Background(), "test", nil)
			if err != nil {
				t.Errorf("Call: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestStaticQueueFull(t *testing.T) {
	// Pool with slow workers and small queue
	p, err := New(newMockFactory(100*time.Millisecond), &mockDispatcher{}, Config{Workers: 1, QueueSize: 2})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()
	p.Start()

	// Fill the queue
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.Call(context.Background(), "test", nil)
		}()
	}

	// Next call should block on queue
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = p.Call(ctx, "test", nil)
	if err != context.DeadlineExceeded {
		t.Logf("expected context deadline exceeded or nil, got: %v", err)
	}

	wg.Wait()
}
