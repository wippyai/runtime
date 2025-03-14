package topology

import (
	"errors"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
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
