package pool

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

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
			_, _ = pool2.Call(testContext(), "test", nil)
		}()
	}

	// Give time for workers to be active
	time.Sleep(20 * time.Millisecond)

	// Third call should timeout (all workers busy, will wait for one)
	ctx := testContext()
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err = pool2.Call(ctx, "test", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Logf("Expected DeadlineExceeded, got: %v", err)
	}

	// Unblock workers
	close(blockCh)
	wg.Wait()
}

type blockingProcess struct {
	blockCh <-chan struct{}
}

func (p *blockingProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *blockingProcess) Step(_ []process.Event, out *process.StepOutput) error {
	<-p.blockCh
	out.Done(nil)
	return nil
}

func (p *blockingProcess) Close()                    {}
func (p *blockingProcess) Send(*relay.Package) error { return nil }
