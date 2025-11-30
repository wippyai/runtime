package clock

import (
	"context"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
)

func TestTimerRegistry(t *testing.T) {
	r := NewTimerRegistry()
	defer r.Close()

	id := r.Start(10 * time.Millisecond)
	if id != 1 {
		t.Errorf("expected first ID to be 1, got %d", id)
	}

	id2 := r.Start(20 * time.Millisecond)
	if id2 != 2 {
		t.Errorf("expected second ID to be 2, got %d", id2)
	}
}

func TestTimerRegistryWait(t *testing.T) {
	r := NewTimerRegistry()
	defer r.Close()

	id := r.Start(5 * time.Millisecond)

	ctx := context.Background()
	fireTime, err := r.Wait(ctx, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fireTime.IsZero() {
		t.Error("expected non-zero fire time")
	}
}

func TestTimerRegistryWaitNotFound(t *testing.T) {
	r := NewTimerRegistry()
	defer r.Close()

	ctx := context.Background()
	_, err := r.Wait(ctx, 999)
	if err != ErrTimerNotFound {
		t.Errorf("expected ErrTimerNotFound, got %v", err)
	}
}

func TestTimerRegistryStop(t *testing.T) {
	r := NewTimerRegistry()
	defer r.Close()

	id := r.Start(time.Hour)

	stopped, err := r.Stop(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stopped {
		t.Error("expected timer to be stopped")
	}

	// Second stop should fail
	_, err = r.Stop(id)
	if err != ErrTimerNotFound {
		t.Errorf("expected ErrTimerNotFound on second stop, got %v", err)
	}
}

func TestTimerRegistryReset(t *testing.T) {
	r := NewTimerRegistry()
	defer r.Close()

	id := r.Start(time.Hour)

	wasActive, err := r.Reset(id, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !wasActive {
		t.Error("expected timer to be active")
	}

	// Wait for reset timer
	ctx := context.Background()
	fireTime, err := r.Wait(ctx, id)
	if err != nil {
		t.Fatalf("wait error: %v", err)
	}
	if fireTime.IsZero() {
		t.Error("expected non-zero fire time")
	}
}

func TestTimerRegistryResetNotFound(t *testing.T) {
	r := NewTimerRegistry()
	defer r.Close()

	_, err := r.Reset(999, time.Second)
	if err != ErrTimerNotFound {
		t.Errorf("expected ErrTimerNotFound, got %v", err)
	}
}

func TestTimerRegistryClose(t *testing.T) {
	r := NewTimerRegistry()

	for i := 0; i < 5; i++ {
		r.Start(time.Hour)
	}

	count := r.Count()
	if count != 5 {
		t.Errorf("expected 5 timers, got %d", count)
	}

	r.Close()

	count = r.Count()
	if count != 0 {
		t.Errorf("expected 0 timers after close, got %d", count)
	}
}

func TestTimerCleanupOnFrameClose(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())

	registry := GetOrCreateTimerRegistry(ctx)
	for i := 0; i < 5; i++ {
		registry.Start(time.Hour)
	}

	count := registry.Count()
	if count != 5 {
		t.Errorf("expected 5 timers, got %d", count)
	}

	// Close frame - should cleanup all timers
	fc.Close()

	count = registry.Count()
	if count != 0 {
		t.Errorf("expected 0 timers after cleanup, got %d", count)
	}
}

func TestTimerRegistryScalability(t *testing.T) {
	const numTimers = 10000

	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer fc.Close()

	registry := GetOrCreateTimerRegistry(ctx)

	// Create many timers
	ids := make([]uint64, numTimers)
	start := time.Now()
	for i := 0; i < numTimers; i++ {
		ids[i] = registry.Start(time.Hour) // Long duration so they don't fire
	}
	createTime := time.Since(start)
	t.Logf("Created %d timers in %v", numTimers, createTime)

	// Verify all created
	if count := registry.Count(); count != numTimers {
		t.Errorf("expected %d timers, got %d", numTimers, count)
	}

	// Stop all in parallel
	start = time.Now()
	var wg sync.WaitGroup
	for i := 0; i < numTimers; i++ {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()
			registry.Stop(id)
		}(ids[i])
	}
	wg.Wait()
	stopTime := time.Since(start)
	t.Logf("Stopped %d timers in %v", numTimers, stopTime)

	// Verify all stopped
	if remaining := registry.Count(); remaining != 0 {
		t.Errorf("expected 0 timers after stop, got %d", remaining)
	}
}

func TestTimerRegistryConcurrentOperations(t *testing.T) {
	const goroutines = 100
	const opsPerGoroutine = 100

	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer fc.Close()

	registry := GetOrCreateTimerRegistry(ctx)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids := make([]uint64, 0, opsPerGoroutine)

			for i := 0; i < opsPerGoroutine; i++ {
				id := registry.Start(time.Hour)
				ids = append(ids, id)
			}

			for _, id := range ids {
				registry.Stop(id)
			}
		}()
	}

	wg.Wait()

	// All should be cleaned up
	if remaining := registry.Count(); remaining != 0 {
		t.Errorf("expected 0 timers after concurrent ops, got %d", remaining)
	}
}

func TestTimerRegistryConcurrentResets(t *testing.T) {
	const goroutines = 50
	const resetsPerGoroutine = 100

	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer fc.Close()

	registry := GetOrCreateTimerRegistry(ctx)

	// Create one timer per goroutine
	ids := make([]uint64, goroutines)
	for i := 0; i < goroutines; i++ {
		ids[i] = registry.Start(time.Hour)
	}

	// Reset all concurrently multiple times
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for i := 0; i < resetsPerGoroutine; i++ {
				registry.Reset(ids[idx], time.Hour)
			}
		}(g)
	}
	wg.Wait()

	// All timers should still exist
	if count := registry.Count(); count != goroutines {
		t.Errorf("expected %d timers after resets, got %d", goroutines, count)
	}

	// Cleanup
	for _, id := range ids {
		registry.Stop(id)
	}
}
