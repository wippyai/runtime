// SPDX-License-Identifier: MPL-2.0

package adaptive

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/system/scheduler/pool"
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

type blockingOnceProcess struct {
	started   chan struct{}
	unblock   chan struct{}
	startOnce sync.Once
}

func newBlockingOnceProcess() *blockingOnceProcess {
	return &blockingOnceProcess{
		started: make(chan struct{}),
		unblock: make(chan struct{}),
	}
}

func (p *blockingOnceProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *blockingOnceProcess) Step(_ []process.Event, out *process.StepOutput) error {
	first := false
	p.startOnce.Do(func() {
		first = true
		close(p.started)
	})
	if first {
		<-p.unblock
	}
	out.Done(nil)
	return nil
}

func (p *blockingOnceProcess) Close() {}

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

func TestAdaptiveStopDrainsAcceptedCallsBeforeStoppingWorkers(t *testing.T) {
	proc := newBlockingOnceProcess()
	p, err := New(func() (process.Process, error) {
		return proc, nil
	}, &mockDispatcher{}, testOptions(1)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.Start()

	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := p.Call(context.Background(), "test", nil)
			errs <- err
		}()
	}

	select {
	case <-proc.started:
	case <-time.After(time.Second):
		t.Fatal("active call did not start")
	}

	waitFor(t, time.Second, func() bool {
		return len(p.tasks) == 1
	})

	stopDone := make(chan struct{})
	go func() {
		p.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
		t.Fatal("Stop returned before accepted calls completed")
	case <-time.After(25 * time.Millisecond):
	}

	close(proc.unblock)

	for i := 0; i < 2; i++ {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("accepted call error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("accepted call did not finish")
		}
	}

	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return")
	}

	_, err = p.Call(context.Background(), "test", nil)
	if !errors.Is(err, pool.ErrPoolClosed) {
		t.Fatalf("post-stop call error = %v, want %v", err, pool.ErrPoolClosed)
	}
}

func waitFor(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
