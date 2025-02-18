package topology

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"sync"
	"testing"
)

type mockUpstream struct {
	mu      sync.Mutex
	sends   map[string][]*pubsub.Batch
	sendErr error
}

func newMockUpstream() *mockUpstream {
	return &mockUpstream{
		sends: make(map[string][]*pubsub.Batch),
	}
}

func (m *mockUpstream) Send(ctx context.Context, pid pubsub.PID, batch *pubsub.Batch) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends[pid.String()] = append(m.sends[pid.String()], batch)
	return nil
}

func (m *mockUpstream) getSends(pid pubsub.PID) []*pubsub.Batch {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sends[pid.String()]
}

func TestMonitor(t *testing.T) {
	ctx := context.Background()
	upstream := newMockUpstream()
	mon := NewMonitor(ctx, upstream)

	pid1 := pubsub.PID{
		Host:   "host1",
		ID:     registry.ID{Name: "test1"},
		UniqID: "1",
	}

	pid2 := pubsub.PID{
		Host:   "host2",
		ID:     registry.ID{Name: "test2"},
		UniqID: "2",
	}

	t.Run("cannot monitor unregistered process", func(t *testing.T) {
		err := mon.Wait(pid2, pid1)
		if err == nil {
			t.Error("expected error when monitoring unregistered process")
		}
		if err != nil && err.Error() != "cannot monitor unregistered pid: {host1|:test1|1}" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("can register process", func(t *testing.T) {
		if err := mon.Register(pid1); err != nil {
			t.Errorf("unexpected error registering process: %v", err)
		}

		// Double registration should pass also
		if err := mon.Register(pid1); err != nil {
			t.Error("expected no error on double registration")
		}
	})

	t.Run("can monitor registered process", func(t *testing.T) {
		if err := mon.Wait(pid2, pid1); err != nil {
			t.Errorf("unexpected error monitoring process: %v", err)
		}

		// Double monitoring should fail
		if err := mon.Wait(pid2, pid1); err == nil {
			t.Error("expected error on double monitoring")
		}
	})

	t.Run("notify sends events to watchers", func(t *testing.T) {
		result := &runtime.Result{
			Payload: payload.New("test result"),
		}

		mon.Notify(pid1, result)

		sends := upstream.getSends(pid2)
		if len(sends) != 1 {
			t.Errorf("expected 1 notification, got %d", len(sends))
		}
	})

	t.Run("release stops monitoring", func(t *testing.T) {
		if err := mon.Release(pid2, pid1); err != nil {
			t.Errorf("unexpected error releasing monitor: %v", err)
		}

		// Can monitor again after release
		if err := mon.Wait(pid2, pid1); err != nil {
			t.Errorf("unexpected error re-monitoring: %v", err)
		}
	})

	t.Run("remove cleans up completely", func(t *testing.T) {
		mon.Remove(pid1)

		// Cannot monitor removed process
		if err := mon.Wait(pid2, pid1); err == nil {
			t.Error("expected error monitoring removed process")
		}
	})
}

func TestMonitorConcurrency(t *testing.T) {
	ctx := context.Background()
	upstream := newMockUpstream()
	mon := NewMonitor(ctx, upstream)

	pid1 := pubsub.PID{
		Host:   "host1",
		ID:     registry.ID{Name: "test1"},
		UniqID: "1",
	}

	// Register the process
	if err := mon.Register(pid1); err != nil {
		t.Fatalf("failed to register process: %v", err)
	}

	// Create multiple watchers
	watcherCount := 10
	watchers := make([]pubsub.PID, watcherCount)
	for i := 0; i < watcherCount; i++ {
		watchers[i] = pubsub.PID{
			Host:   "watcher",
			ID:     registry.ID{Name: "watcher"},
			UniqID: string(rune('0' + i)),
		}
	}

	// Concurrently attach watchers
	var wg sync.WaitGroup
	wg.Add(watcherCount)
	for _, watcher := range watchers {
		w := watcher
		go func() {
			defer wg.Done()
			if err := mon.Wait(w, pid1); err != nil {
				t.Errorf("unexpected error attaching watcher: %v", err)
			}
		}()
	}
	wg.Wait()

	// Notify all watchers
	result := &runtime.Result{
		Payload: payload.New("test result"),
	}
	mon.Notify(pid1, result)

	// Verify all watchers received notification
	for _, watcher := range watchers {
		sends := upstream.getSends(watcher)
		if len(sends) != 1 {
			t.Errorf("watcher %s: expected 1 notification, got %d", watcher.String(), len(sends))
		}
	}
}

func TestMonitorUpstreamError(t *testing.T) {
	ctx := context.Background()
	upstream := newMockUpstream()
	upstream.sendErr = errors.New("send error")
	mon := NewMonitor(ctx, upstream)

	pid1 := pubsub.PID{
		Host:   "host1",
		ID:     registry.ID{Name: "test1"},
		UniqID: "1",
	}

	pid2 := pubsub.PID{
		Host:   "host2",
		ID:     registry.ID{Name: "test2"},
		UniqID: "2",
	}

	// Register and monitor
	if err := mon.Register(pid1); err != nil {
		t.Fatalf("failed to register process: %v", err)
	}
	if err := mon.Wait(pid2, pid1); err != nil {
		t.Fatalf("failed to monitor process: %v", err)
	}

	// Notify should not panic on upstream error
	result := &runtime.Result{
		Payload: payload.New("test result"),
	}
	mon.Notify(pid1, result)
}
