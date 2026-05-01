// SPDX-License-Identifier: MPL-2.0

package static

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
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Logf("expected context deadline exceeded or nil, got: %v", err)
	}

	wg.Wait()
}

func TestStaticStopDrainsAcceptedCallsBeforeStoppingWorkers(t *testing.T) {
	proc := newBlockingOnceProcess()
	p, err := New(func() (process.Process, error) {
		return proc, nil
	}, &mockDispatcher{}, Config{Workers: 1, QueueSize: 1})
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
