package topology

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/stretchr/testify/assert"
)

type mockUpstream struct {
	mu      sync.Mutex
	sends   map[string][]*pubsub.Package
	sendErr error
}

func newMockUpstream() *mockUpstream {
	return &mockUpstream{
		sends: make(map[string][]*pubsub.Package),
	}
}

func (m *mockUpstream) Send(pkg *pubsub.Package) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends[pkg.Target.String()] = append(m.sends[pkg.Target.String()], pkg)
	return nil
}

func (m *mockUpstream) getSends(pid pubsub.PID) []*pubsub.Package {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sends[pid.String()]
}

func (m *mockUpstream) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends = make(map[string][]*pubsub.Package)
}

func TestTopology_BasicFunctionality(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream)

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
		err := topo.Wait(pid2, pid1)
		if err == nil {
			t.Error("expected error when monitoring unregistered process")
		}
		if err != nil && err.Error() != "cannot monitor unregistered pid: {host1|:test1|1}" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("can register process", func(t *testing.T) {
		if err := topo.Register(pid1); err != nil {
			t.Errorf("unexpected error registering process: %v", err)
		}

		// Double registration should pass also
		if err := topo.Register(pid1); err != nil {
			t.Error("expected no error on double registration")
		}
	})

	t.Run("can monitor registered process", func(t *testing.T) {
		if err := topo.Wait(pid2, pid1); err != nil {
			t.Errorf("unexpected error monitoring process: %v", err)
		}

		// Double monitoring should fail
		if err := topo.Wait(pid2, pid1); err == nil {
			t.Error("expected error on double monitoring")
		}
	})

	t.Run("notify sends events to watchers", func(t *testing.T) {
		result := &runtime.Result{
			Value: payload.New("test result"),
		}

		topo.Notify(pid1, result)

		sends := upstream.getSends(pid2)
		if len(sends) != 1 {
			t.Errorf("expected 1 notification, got %d", len(sends))
		}
	})

	t.Run("release stops monitoring", func(t *testing.T) {
		if err := topo.Release(pid2, pid1); err != nil {
			t.Errorf("unexpected error releasing monitor: %v", err)
		}

		// Can monitor again after release
		if err := topo.Wait(pid2, pid1); err != nil {
			t.Errorf("unexpected error re-monitoring: %v", err)
		}
	})

	t.Run("remove cleans up completely", func(t *testing.T) {
		topo.Remove(pid1)

		// Cannot monitor removed process
		if err := topo.Wait(pid2, pid1); err == nil {
			t.Error("expected error monitoring removed process")
		}
	})
}

func TestTopology_LinkFunctionality(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream)

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

	pid3 := pubsub.PID{
		Host:   "host3",
		ID:     registry.ID{Name: "test3"},
		UniqID: "3",
	}

	// Register processes
	assert.NoError(t, topo.Register(pid1))
	assert.NoError(t, topo.Register(pid2))
	assert.NoError(t, topo.Register(pid3))

	t.Run("cannot link unregistered process", func(t *testing.T) {
		unregisteredPid := pubsub.PID{
			Host:   "host4",
			ID:     registry.ID{Name: "test4"},
			UniqID: "4",
		}

		err := topo.Link(pid1, unregisteredPid)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot link unregistered pid")
	})

	t.Run("can link registered processes", func(t *testing.T) {
		upstream.reset()

		err := topo.Link(pid1, pid2)
		assert.NoError(t, err)

		// Link notifications are no longer sent according to updated spec
		// No need to check for notifications
	})

	t.Run("linking is bidirectional", func(t *testing.T) {
		links1 := topo.GetLinks(pid1)
		links2 := topo.GetLinks(pid2)

		assert.Equal(t, 1, len(links1), "pid1 should have 1 link")
		assert.Equal(t, 1, len(links2), "pid2 should have 1 link")

		assert.Equal(t, pid2, links1[0], "pid1 should be linked to pid2")
		assert.Equal(t, pid1, links2[0], "pid2 should be linked to pid1")
	})

	t.Run("can establish multiple links", func(t *testing.T) {
		err := topo.Link(pid1, pid3)
		assert.NoError(t, err)

		links1 := topo.GetLinks(pid1)
		assert.Equal(t, 2, len(links1), "pid1 should have 2 links")

		// Check if pid3 is linked to pid1
		links3 := topo.GetLinks(pid3)
		assert.Equal(t, 1, len(links3), "pid3 should have 1 link")
		assert.Equal(t, pid1, links3[0], "pid3 should be linked to pid1")
	})

	t.Run("can unlink processes", func(t *testing.T) {
		upstream.reset()

		err := topo.Unlink(pid1, pid2)
		assert.NoError(t, err)

		// Unlink notifications are no longer sent according to updated spec
		// No need to check for notifications

		// Verify links are removed
		links1 := topo.GetLinks(pid1)
		links2 := topo.GetLinks(pid2)

		assert.Equal(t, 1, len(links1), "pid1 should have 1 link")
		assert.Equal(t, 0, len(links2), "pid2 should have 0 links")

		assert.Equal(t, pid3, links1[0], "pid1 should still be linked to pid3")
	})

	t.Run("notify propagates to linked processes for abnormal exits", func(t *testing.T) {
		upstream.reset()

		// Create result with an error to trigger abnormal exit
		testErr := errors.New("test error")
		result := &runtime.Result{
			Value: payload.New("test result"),
			Error: testErr,
		}

		topo.Notify(pid1, result)

		// Check that linked pid3 received notification for abnormal exit
		sends3 := upstream.getSends(pid3)
		assert.Equal(t, 1, len(sends3), "pid3 should receive exit notification for abnormal exit")

		// Check that unlinked pid2 didn't receive notification
		sends2 := upstream.getSends(pid2)
		assert.Equal(t, 0, len(sends2), "pid2 should not receive exit notification")

		// Reset and test normal exit
		upstream.reset()

		// Create result without an error for normal exit
		normalResult := &runtime.Result{
			Value: payload.New("normal exit"),
		}

		topo.Notify(pid1, normalResult)

		// Check that linked pid3 did not receive notification for normal exit
		sends3 = upstream.getSends(pid3)
		assert.Equal(t, 0, len(sends3), "pid3 should not receive exit notification for normal exit")
	})

	t.Run("removing process cleans up links", func(t *testing.T) {
		upstream.reset()

		topo.Remove(pid1)

		// Verify all links to pid1 are removed
		links3 := topo.GetLinks(pid3)
		assert.Equal(t, 0, len(links3), "pid3 should have 0 links after pid1 is removed")

		// Unlink notifications are no longer sent according to updated spec
		// No need to check for notifications
	})
}

func TestTopology_Concurrency(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream)

	mainPid := pubsub.PID{
		Host:   "host1",
		ID:     registry.ID{Name: "main"},
		UniqID: "main",
	}

	// Register the main process
	assert.NoError(t, topo.Register(mainPid))

	// Create multiple PIDs
	workerCount := 10
	workers := make([]pubsub.PID, workerCount)
	for i := 0; i < workerCount; i++ {
		workers[i] = pubsub.PID{
			Host:   "worker",
			ID:     registry.ID{Name: "worker"},
			UniqID: string(rune('0' + i)),
		}
		assert.NoError(t, topo.Register(workers[i]))
	}

	t.Run("concurrent linking", func(t *testing.T) {
		upstream.reset()

		var wg sync.WaitGroup
		wg.Add(workerCount)

		for _, worker := range workers {
			w := worker
			go func() {
				defer wg.Done()
				err := topo.Link(mainPid, w)
				assert.NoError(t, err)
			}()
		}

		wg.Wait()

		// Verify all workers are linked to main
		links := topo.GetLinks(mainPid)
		assert.Equal(t, workerCount, len(links), "mainPid should be linked to all workers")
	})

	t.Run("exit propagation to multiple links for abnormal exit", func(t *testing.T) {
		upstream.reset()

		// Create result with an error to trigger abnormal exit
		result := &runtime.Result{
			Value: payload.New("main process exited with error"),
			Error: errors.New("test abnormal exit"),
		}

		topo.Notify(mainPid, result)

		// Verify all workers received exit notification
		for _, worker := range workers {
			sends := upstream.getSends(worker)
			assert.Equal(t, 1, len(sends), "worker should receive exit notification for abnormal exit")
		}

		// Test normal exit doesn't send notifications
		upstream.reset()

		// Create result without an error for normal exit
		normalResult := &runtime.Result{
			Value: payload.New("main process exited normally"),
		}

		topo.Notify(mainPid, normalResult)

		// Verify workers didn't receive notifications for normal exit
		for _, worker := range workers {
			sends := upstream.getSends(worker)
			assert.Equal(t, 0, len(sends), "worker should not receive exit notification for normal exit")
		}
	})

	t.Run("concurrent unlinking", func(t *testing.T) {
		upstream.reset()

		var wg sync.WaitGroup
		wg.Add(workerCount)

		for _, worker := range workers {
			w := worker
			go func() {
				defer wg.Done()
				err := topo.Unlink(mainPid, w)
				assert.NoError(t, err)
			}()
		}

		wg.Wait()

		// Verify all links are removed
		links := topo.GetLinks(mainPid)
		assert.Equal(t, 0, len(links), "mainPid should have no links")
	})
}

func TestTopology_UpstreamError(t *testing.T) {
	upstream := newMockUpstream()
	upstream.sendErr = errors.New("send error")
	topo := NewTopology(upstream)

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
	assert.NoError(t, topo.Register(pid1))
	assert.NoError(t, topo.Register(pid2))
	assert.NoError(t, topo.Wait(pid2, pid1))
	assert.NoError(t, topo.Link(pid1, pid2))

	// Notify should not panic on upstream error
	result := &runtime.Result{
		Value: payload.New("test result"),
		Error: errors.New("error for testing"),
	}

	// This should not panic even with upstream error
	topo.Notify(pid1, result)
}

func TestTopology_EdgeCases(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream)

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

	t.Run("register empty PID", func(t *testing.T) {
		emptyPID := pubsub.PID{}
		err := topo.Register(emptyPID)
		assert.NoError(t, err, "registering empty PID should not error")
	})

	t.Run("wait with empty caller PID", func(t *testing.T) {
		assert.NoError(t, topo.Register(pid1))
		emptyPID := pubsub.PID{}
		err := topo.Wait(emptyPID, pid1)
		assert.NoError(t, err, "waiting with empty caller PID should not error")
	})

	t.Run("release non-existent monitor", func(t *testing.T) {
		err := topo.Release(pid2, pid1)
		assert.NoError(t, err, "releasing non-existent monitor should not error")
	})

	t.Run("unlink non-existent processes", func(t *testing.T) {
		err := topo.Unlink(pid1, pid2)
		assert.NoError(t, err, "unlinking non-existent processes should not error")
	})

	t.Run("get links for non-existent process", func(t *testing.T) {
		links := topo.GetLinks(pid1)
		assert.Empty(t, links, "non-existent process should have no links")
	})

	t.Run("notify for non-existent process", func(_ *testing.T) {
		result := &runtime.Result{
			Value: payload.New("test result"),
		}
		// Should not panic
		topo.Notify(pid1, result)
	})

	t.Run("remove non-existent process", func(_ *testing.T) {
		// Should not panic
		topo.Remove(pid1)
	})
}

func TestTopology_ConcurrentOperations(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream)

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

	// Register both processes
	assert.NoError(t, topo.Register(pid1))
	assert.NoError(t, topo.Register(pid2))

	t.Run("concurrent link and unlink", func(t *testing.T) {
		var wg sync.WaitGroup
		iterations := 100

		for i := 0; i < iterations; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				_ = topo.Link(pid1, pid2)
			}()
			go func() {
				defer wg.Done()
				_ = topo.Unlink(pid1, pid2)
			}()
		}

		wg.Wait()
		// Verify final state is consistent
		// Since we have equal numbers of Link and Unlink operations,
		// the final state is indeterminate, but we can verify data structure integrity
		links1 := topo.GetLinks(pid1)
		links2 := topo.GetLinks(pid2)

		// Check if the processes are linked bidirectionally
		// If pid1 has pid2 in its links, then pid2 should have pid1 in its links
		pid1HasPid2 := false
		for _, link := range links1 {
			if link.String() == pid2.String() {
				pid1HasPid2 = true
				break
			}
		}

		pid2HasPid1 := false
		for _, link := range links2 {
			if link.String() == pid1.String() {
				pid2HasPid1 = true
				break
			}
		}

		// The links should be consistent - either both processes are linked to each other
		// or neither is linked to the other
		assert.Equal(t, pid1HasPid2, pid2HasPid1, "links should be bidirectional - if pid1 links to pid2, pid2 should link to pid1")
	})

	t.Run("concurrent wait and release", func(t *testing.T) {
		// First ensure pid2 is not monitoring pid1
		_ = topo.Release(pid2, pid1)

		var wg sync.WaitGroup
		iterations := 100
		start := make(chan struct{})
		done := make(chan struct{})

		// Start all goroutines but make them wait for the start signal
		for i := 0; i < iterations; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_ = topo.Wait(pid2, pid1)
			}()
			go func() {
				defer wg.Done()
				<-start
				_ = topo.Release(pid2, pid1)
			}()
		}

		// Signal all goroutines to start
		close(start)

		// Wait for all goroutines to complete in a separate goroutine
		go func() {
			wg.Wait()
			close(done)
		}()

		// Wait for completion with timeout
		select {
		case <-done:
			// All goroutines completed
		case <-time.After(5 * time.Second):
			t.Fatal("test timed out waiting for goroutines to complete")
		}

		// Add a small delay to ensure all operations are processed
		time.Sleep(100 * time.Millisecond)

		// Verify that the final state is consistent
		// Since we have equal numbers of Wait and Release operations,
		// the final state is indeterminate, but we can verify the data structure integrity
		value, ok := topo.monitors.Load(pid1.String())
		if ok {
			watchers := value.(*sync.Map)
			// Count how many watchers exist for pid1
			watcherCount := 0
			watchers.Range(func(_, _ interface{}) bool {
				watcherCount++
				return true
			})

			// Check if pid2 is monitoring pid1
			_, stillMonitoring := watchers.Load(pid2.String())

			// The final state should be consistent - either pid2 is monitoring pid1
			// (in which case there should be exactly 1 watcher) or it's not
			// (in which case there should be 0 watchers or pid2 should not be in the watchers)
			if stillMonitoring {
				assert.Equal(t, 1, watcherCount, "if pid2 is monitoring pid1, there should be exactly 1 watcher")
			} else {
				assert.Equal(t, 0, watcherCount, "if pid2 is not monitoring pid1, there should be no watchers")
			}
		}
		// No monitors for pid1, which is a valid final state
		// This means all Release operations completed after all Wait operations
	})
}

func TestTopology_NotificationScenarios(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream)

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

	pid3 := pubsub.PID{
		Host:   "host3",
		ID:     registry.ID{Name: "test3"},
		UniqID: "3",
	}

	// Register all processes
	assert.NoError(t, topo.Register(pid1))
	assert.NoError(t, topo.Register(pid2))
	assert.NoError(t, topo.Register(pid3))

	// Set up monitoring and linking
	assert.NoError(t, topo.Wait(pid2, pid1))
	assert.NoError(t, topo.Link(pid1, pid3))

	t.Run("notify with empty result", func(t *testing.T) {
		upstream.reset()
		emptyResult := &runtime.Result{}
		topo.Notify(pid1, emptyResult)

		// Check notifications
		sends2 := upstream.getSends(pid2)
		assert.Equal(t, 1, len(sends2), "monitor should receive notification")

		sends3 := upstream.getSends(pid3)
		assert.Equal(t, 0, len(sends3), "linked process should not receive notification for normal exit")
	})

	t.Run("notify after process removal", func(t *testing.T) {
		upstream.reset()
		topo.Remove(pid1)

		result := &runtime.Result{
			Value: payload.New("test result"),
		}
		topo.Notify(pid1, result)

		// Verify no notifications are sent
		sends2 := upstream.getSends(pid2)
		assert.Equal(t, 0, len(sends2), "no notifications should be sent for removed process")

		sends3 := upstream.getSends(pid3)
		assert.Equal(t, 0, len(sends3), "no notifications should be sent for removed process")
	})
}
