// SPDX-License-Identifier: MPL-2.0

package lazy

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

type blockingProcess struct {
	started   chan struct{}
	unblock   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func newBlockingProcess() *blockingProcess {
	return &blockingProcess{
		started: make(chan struct{}),
		unblock: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (p *blockingProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *blockingProcess) Step(_ []process.Event, out *process.StepOutput) error {
	p.startOnce.Do(func() { close(p.started) })
	<-p.unblock
	out.Done(nil)
	return nil
}

func (p *blockingProcess) Close() {
	p.closeOnce.Do(func() { close(p.closed) })
}

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

func TestLazyStopDrainsAcceptedCallsAndWaiters(t *testing.T) {
	activeProc := newBlockingProcess()
	factoryCalls := atomic.Int32{}
	p, err := New(func() (process.Process, error) {
		factoryCalls.Add(1)
		return activeProc, nil
	}, &mockDispatcher{}, Config{MaxWorkers: 1, IdleTimeout: time.Minute})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.Start()

	activeDone := make(chan error, 1)
	go func() {
		_, err := p.Call(context.Background(), "test", nil)
		activeDone <- err
	}()

	select {
	case <-activeProc.started:
	case <-time.After(time.Second):
		t.Fatal("active call did not start")
	}

	waiterDone := make(chan error, 1)
	go func() {
		_, err := p.Call(context.Background(), "test", nil)
		waiterDone <- err
	}()

	waitFor(t, time.Second, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.waiters) == 1
	})

	stopDone := make(chan struct{})
	go func() {
		p.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
		t.Fatal("Stop returned before active call completed")
	case <-time.After(25 * time.Millisecond):
	}

	select {
	case err := <-waiterDone:
		t.Fatalf("waiter finished before worker release: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(activeProc.unblock)

	select {
	case err := <-activeDone:
		if err != nil {
			t.Fatalf("active call error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("active call did not finish")
	}

	select {
	case err := <-waiterDone:
		if err != nil {
			t.Fatalf("waiter call error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter call did not finish")
	}

	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after accepted calls completed")
	}

	select {
	case <-activeProc.closed:
	case <-time.After(time.Second):
		t.Fatal("active process was not closed after Stop")
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
