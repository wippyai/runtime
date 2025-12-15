package clock

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
)

func TestTimerRegistry_New(t *testing.T) {
	r := newTimerRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if r.count() != 0 {
		t.Errorf("expected 0 timers, got %d", r.count())
	}
}

func TestTimerRegistry_StartWithCallback(t *testing.T) {
	r := newTimerRegistry()
	defer r.close()

	var called atomic.Bool
	id := r.startWithCallback(10*time.Millisecond, func() {
		called.Store(true)
	})

	if id == 0 {
		t.Error("expected non-zero timer ID")
	}
	if r.count() != 1 {
		t.Errorf("expected 1 timer, got %d", r.count())
	}

	time.Sleep(50 * time.Millisecond)

	if !called.Load() {
		t.Error("callback was not called")
	}
}

func TestTimerRegistry_Wait(t *testing.T) {
	r := newTimerRegistry()
	defer r.close()

	id := r.startWithCallback(10*time.Millisecond, nil)

	ctx := context.Background()
	fireTime, err := r.wait(ctx, id)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if fireTime.IsZero() {
		t.Error("expected non-zero fire time")
	}
}

func TestTimerRegistry_WaitNotFound(t *testing.T) {
	r := newTimerRegistry()
	defer r.close()

	ctx := context.Background()
	_, err := r.wait(ctx, 999)
	if !errors.Is(err, clockapi.ErrTimerNotFound) {
		t.Errorf("expected ErrTimerNotFound, got %v", err)
	}
}

func TestTimerRegistry_WaitContextCanceled(t *testing.T) {
	r := newTimerRegistry()
	defer r.close()

	id := r.startWithCallback(time.Hour, nil)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := r.wait(ctx, id)
		done <- err
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for wait to return")
	}
}

func TestTimerRegistry_Stop(t *testing.T) {
	r := newTimerRegistry()
	defer r.close()

	id := r.startWithCallback(time.Hour, nil)

	stopped, err := r.stop(id)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !stopped {
		t.Error("expected timer to be stopped")
	}
	if r.count() != 0 {
		t.Errorf("expected 0 timers, got %d", r.count())
	}
}

func TestTimerRegistry_StopNotFound(t *testing.T) {
	r := newTimerRegistry()
	defer r.close()

	_, err := r.stop(999)
	if !errors.Is(err, clockapi.ErrTimerNotFound) {
		t.Errorf("expected ErrTimerNotFound, got %v", err)
	}
}

func TestTimerRegistry_StopAlreadyFired(t *testing.T) {
	r := newTimerRegistry()
	defer r.close()

	// Use a callback so timer removes itself from map after firing
	var fired atomic.Bool
	id := r.startWithCallback(5*time.Millisecond, func() {
		fired.Store(true)
	})

	time.Sleep(20 * time.Millisecond)

	if !fired.Load() {
		t.Fatal("expected timer to fire")
	}

	_, err := r.stop(id)
	if !errors.Is(err, clockapi.ErrTimerNotFound) {
		t.Errorf("expected ErrTimerNotFound for fired timer, got %v", err)
	}
}

func TestTimerRegistry_Reset(t *testing.T) {
	r := newTimerRegistry()
	defer r.close()

	id := r.startWithCallback(time.Hour, nil)

	wasActive, err := r.reset(id, 10*time.Millisecond)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !wasActive {
		t.Error("expected timer to be active")
	}
}

func TestTimerRegistry_ResetNotFound(t *testing.T) {
	r := newTimerRegistry()
	defer r.close()

	_, err := r.reset(999, time.Second)
	if !errors.Is(err, clockapi.ErrTimerNotFound) {
		t.Errorf("expected ErrTimerNotFound, got %v", err)
	}
}

func TestTimerRegistry_ResetStopped(t *testing.T) {
	r := newTimerRegistry()
	defer r.close()

	id := r.startWithCallback(time.Hour, nil)
	_, _ = r.stop(id)

	_, err := r.reset(id, time.Second)
	if !errors.Is(err, clockapi.ErrTimerNotFound) {
		t.Errorf("expected ErrTimerNotFound for stopped timer, got %v", err)
	}
}

func TestTimerRegistry_Close(t *testing.T) {
	r := newTimerRegistry()

	for i := 0; i < 10; i++ {
		r.startWithCallback(time.Hour, nil)
	}

	if r.count() != 10 {
		t.Errorf("expected 10 timers, got %d", r.count())
	}

	r.close()

	if r.count() != 0 {
		t.Errorf("expected 0 timers after close, got %d", r.count())
	}
}

func TestTimerRegistry_Concurrent(t *testing.T) {
	r := newTimerRegistry()
	defer r.close()

	const goroutines = 10
	const timersPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < timersPerGoroutine; j++ {
				id := r.startWithCallback(time.Hour, nil)
				_, _ = r.stop(id)
			}
		}()
	}

	wg.Wait()
}

func TestTimerRegistry_GetShard(t *testing.T) {
	r := newTimerRegistry()

	shard1 := r.getShard(1)
	shard2 := r.getShard(65)

	if shard1 != shard2 {
		t.Error("expected same shard for IDs 1 and 65 (mod 64)")
	}

	shard3 := r.getShard(2)
	if shard1 == shard3 {
		t.Error("expected different shards for IDs 1 and 2")
	}
}

func TestTimerRegistry_WaitAfterStopped(t *testing.T) {
	r := newTimerRegistry()
	defer r.close()

	id := r.startWithCallback(time.Hour, nil)

	entry := r.shards[id&(timerShardCount-1)].timers[id]
	entry.stopped.Store(true)

	ctx := context.Background()
	_, err := r.wait(ctx, id)
	if !errors.Is(err, clockapi.ErrTimerNotFound) {
		t.Errorf("expected ErrTimerNotFound for stopped entry, got %v", err)
	}
}
