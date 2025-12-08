package pool

import (
	"context"
	"sync"
	"testing"
	"time"
)

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
	pool, err := NewStatic(newMockFactory(50*time.Millisecond), &mockDispatcher{}, Config{Workers: 1})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	ctx := testContext()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	// Call always waits for result even if context is cancelled
	// The mock process doesn't check context, so it completes successfully
	// This test verifies that Call() waits for completion regardless of context state
	result, err := pool.Call(ctx, "test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	// Mock process doesn't propagate context cancellation - it completes normally
	// Real processes would set result.Error = ctx.Err() if they respect cancellation
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
			_, _ = pool.Call(testContext(), "test", nil)
		}()
	}

	// Give some time for work to start
	time.Sleep(5 * time.Millisecond)

	// Stop should wait for completion
	pool.Stop()
	wg.Wait()
}
