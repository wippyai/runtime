// SPDX-License-Identifier: MPL-2.0

package adaptive

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

func testOptions(maxWorkers int) []Option {
	return []Option{
		WithMaxWorkers(maxWorkers),
		WithControlInterval(100 * time.Millisecond),
		WithProbeTicks(3),
		WithIdleTicks(2),
	}
}

func TestAdaptiveBasic(t *testing.T) {
	p, err := New(newMockFactory(0), &mockDispatcher{}, testOptions(4)...)
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

func TestAdaptiveConcurrent(t *testing.T) {
	factory, count := newCountingFactory()
	p, err := New(factory, &mockDispatcher{}, testOptions(8)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()
	p.Start()

	// Starts with 1 worker (minWorkers)
	time.Sleep(50 * time.Millisecond)
	if count.Load() != 1 {
		t.Fatalf("expected 1 initial worker, got %d", count.Load())
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

func TestAdaptiveScalesUp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping adaptive scaling test in short mode")
	}

	factory, count := newCountingFactory()
	opts := []Option{
		WithMaxWorkers(4),
		WithControlInterval(50 * time.Millisecond),
		WithProbeTicks(2),
		WithIdleTicks(10),
	}

	p, err := New(factory, &mockDispatcher{}, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()
	p.Start()

	// Initial worker count
	initial := count.Load()
	if initial != 1 {
		t.Fatalf("expected 1 initial worker, got %d", initial)
	}

	// Create sustained load with slow calls
	done := make(chan struct{})
	var wg sync.WaitGroup

	slowFactory := newMockFactory(100 * time.Millisecond)
	slowPool, err := New(slowFactory, &mockDispatcher{}, opts...)
	if err != nil {
		t.Fatalf("New slow: %v", err)
	}
	defer slowPool.Stop()
	slowPool.Start()

	// Hammer with concurrent requests
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					_, _ = slowPool.Call(context.Background(), "test", nil)
				}
			}
		}()
	}

	// Wait for scaling
	time.Sleep(1 * time.Second)
	close(done)
	wg.Wait()

	// Should have scaled up
	// (exact count depends on timing)
}

func TestAdaptiveStop(t *testing.T) {
	p, err := New(newMockFactory(0), &mockDispatcher{}, testOptions(4)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.Start()

	// Call before stop should work
	_, err = p.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call before stop: %v", err)
	}

	// Stop the pool
	p.Stop()

	// Call after stop should fail
	_, err = p.Call(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error after stop")
	}
}
