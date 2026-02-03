package topology

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
)

type mockUpstream struct {
	sendErr error
	sends   map[string][]*relay.Package
	mu      sync.Mutex
}

func newMockUpstream() *mockUpstream {
	return &mockUpstream{
		sends: make(map[string][]*relay.Package),
	}
}

func (m *mockUpstream) Send(pkg *relay.Package) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends[pkg.Target.String()] = append(m.sends[pkg.Target.String()], pkg)
	return nil
}

func (m *mockUpstream) getSends(pid pid.PID) []*relay.Package {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sends[pid.String()]
}

func (m *mockUpstream) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends = make(map[string][]*relay.Package)
}

func TestTopology_BasicFunctionality(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	pid1 := pid.PID{
		Host:   "host1",
		UniqID: "1",
	}
	pid1 = pid1.Precomputed()

	pid2 := pid.PID{
		Host:   "host2",
		UniqID: "2",
	}
	pid2 = pid2.Precomputed()

	t.Run("cannot monitor unregistered process", func(t *testing.T) {
		err := topo.Monitor(pid2, pid1)
		assert.Error(t, err, "expected error when monitoring unregistered process")
		assert.True(t, errors.Is(err, topology.ErrPIDNotRegistered), "expected ErrPIDNotRegistered")
	})

	t.Run("can register process", func(t *testing.T) {
		if err := topo.Register(pid1); err != nil {
			t.Errorf("unexpected error registering process: %v", err)
		}

		// Double registration should fail
		if err := topo.Register(pid1); err == nil {
			t.Error("expected error on double registration")
		}
	})

	t.Run("can monitor registered process", func(t *testing.T) {
		if err := topo.Monitor(pid2, pid1); err != nil {
			t.Errorf("unexpected error monitoring process: %v", err)
		}

		// Double monitoring should fail
		if err := topo.Monitor(pid2, pid1); err == nil {
			t.Error("expected error on double monitoring")
		}
	})

	t.Run("notify sends events to watchers", func(t *testing.T) {
		result := &runtime.Result{
			Value: payload.New("test result"),
		}

		topo.Complete(pid1, result)

		sends := upstream.getSends(pid2)
		if len(sends) != 1 {
			t.Errorf("expected 1 notification, got %d", len(sends))
		}
	})

	t.Run("release stops monitoring", func(t *testing.T) {
		// Re-register pid1 (was removed by Complete in previous test)
		_ = topo.Register(pid1)
		_ = topo.Monitor(pid2, pid1)

		if err := topo.Demonitor(pid2, pid1); err != nil {
			t.Errorf("unexpected error releasing monitor: %v", err)
		}

		// Can monitor again after release
		if err := topo.Monitor(pid2, pid1); err != nil {
			t.Errorf("unexpected error re-monitoring: %v", err)
		}
	})

	t.Run("remove cleans up completely", func(t *testing.T) {
		topo.Complete(pid1, &runtime.Result{})

		// Cannot monitor removed process
		if err := topo.Monitor(pid2, pid1); err == nil {
			t.Error("expected error monitoring removed process")
		}
	})
}

func TestTopology_LinkFunctionality(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	pid1 := pid.PID{
		Host:   "host1",
		UniqID: "1",
	}
	pid1 = pid1.Precomputed()

	pid2 := pid.PID{
		Host:   "host2",
		UniqID: "2",
	}
	pid2 = pid2.Precomputed()

	pid3 := pid.PID{
		Host:   "host3",
		UniqID: "3",
	}
	pid3 = pid3.Precomputed()

	// Register processes
	assert.NoError(t, topo.Register(pid1))
	assert.NoError(t, topo.Register(pid2))
	assert.NoError(t, topo.Register(pid3))

	t.Run("cannot link unregistered process", func(t *testing.T) {
		unregisteredPid := pid.PID{
			Host:   "host4",
			UniqID: "4",
		}

		err := topo.Link(pid1, unregisteredPid)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, topology.ErrPIDNotRegistered), "expected ErrPIDNotRegistered")
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

		topo.Complete(pid1, result)

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

		topo.Complete(pid1, normalResult)

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
	topo := NewTopology(upstream, "local")

	mainPid := pid.PID{
		Host:   "host1",
		UniqID: "main",
	}

	// Register the main process
	assert.NoError(t, topo.Register(mainPid))

	// Create multiple PIDs
	workerCount := 10
	workers := make([]pid.PID, workerCount)
	for i := 0; i < workerCount; i++ {
		workers[i] = pid.PID{
			Host:   "worker",
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

		topo.Complete(mainPid, result)

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

		topo.Complete(mainPid, normalResult)

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
	topo := NewTopology(upstream, "local")

	pid1 := pid.PID{
		Host:   "host1",
		UniqID: "1",
	}

	pid2 := pid.PID{
		Host:   "host2",
		UniqID: "2",
	}

	// Register and monitor
	assert.NoError(t, topo.Register(pid1))
	assert.NoError(t, topo.Register(pid2))
	assert.NoError(t, topo.Monitor(pid2, pid1))
	assert.NoError(t, topo.Link(pid1, pid2))

	// Notify should not panic on upstream error
	result := &runtime.Result{
		Value: payload.New("test result"),
		Error: errors.New("error for testing"),
	}

	// This should not panic even with upstream error
	topo.Complete(pid1, result)
}

func TestTopology_EdgeCases(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	pid1 := pid.PID{
		Host:   "host1",
		UniqID: "1",
	}

	pid2 := pid.PID{
		Host:   "host2",
		UniqID: "2",
	}

	t.Run("register empty PID", func(t *testing.T) {
		emptyPID := pid.PID{}
		err := topo.Register(emptyPID)
		assert.NoError(t, err, "registering empty PID should not error")
	})

	t.Run("wait with empty caller PID", func(t *testing.T) {
		assert.NoError(t, topo.Register(pid1))
		emptyPID := pid.PID{}
		err := topo.Monitor(emptyPID, pid1)
		assert.NoError(t, err, "waiting with empty caller PID should not error")
	})

	t.Run("release non-existent monitor", func(t *testing.T) {
		err := topo.Demonitor(pid2, pid1)
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
		topo.Complete(pid1, result)
	})

	t.Run("remove non-existent process", func(_ *testing.T) {
		// Should not panic
		topo.Remove(pid1)
	})
}

func TestTopology_ConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent operations test in short mode")
	}

	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	pid1 := pid.PID{
		Host:   "host1",
		UniqID: "1",
	}

	pid2 := pid.PID{
		Host:   "host2",
		UniqID: "2",
	}

	// Register both processes
	assert.NoError(t, topo.Register(pid1))
	assert.NoError(t, topo.Register(pid2))

	t.Run("concurrent link and unlink", func(t *testing.T) {
		var wg sync.WaitGroup
		iterations := 100

		// Use context with timeout to prevent test hanging
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

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

		// Wait for all operations to complete with context timeout
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// All operations completed
		case <-ctx.Done():
			t.Fatal("test timed out waiting for operations to complete")
		}

		// Small delay for operations to settle
		time.Sleep(50 * time.Millisecond)

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
		_ = topo.Demonitor(pid2, pid1)

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
				_ = topo.Monitor(pid2, pid1)
			}()
			go func() {
				defer wg.Done()
				<-start
				_ = topo.Demonitor(pid2, pid1)
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

		// Small delay for operations to settle
		time.Sleep(50 * time.Millisecond)

		// Verify that the final state is consistent by calling Complete and checking notifications.
		// Since we have equal numbers of Monitor and Demonitor operations,
		// the final state is indeterminate (0 or 1 notification), but internal state should be consistent.
		upstream.reset()
		topo.Complete(pid1, &runtime.Result{})
		notifications := upstream.getSends(pid2)

		// The final state should be consistent - either pid2 receives 0 or 1 notification.
		assert.True(t, len(notifications) == 0 || len(notifications) == 1,
			"should have 0 or 1 notification, got %d", len(notifications))
	})
}

func TestTopology_NotificationScenarios(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	pid1 := pid.PID{
		Host:   "host1",
		UniqID: "1",
	}

	pid2 := pid.PID{
		Host:   "host2",
		UniqID: "2",
	}

	pid3 := pid.PID{
		Host:   "host3",
		UniqID: "3",
	}

	// Register all processes
	assert.NoError(t, topo.Register(pid1))
	assert.NoError(t, topo.Register(pid2))
	assert.NoError(t, topo.Register(pid3))

	// Set up monitoring and linking
	assert.NoError(t, topo.Monitor(pid2, pid1))
	assert.NoError(t, topo.Link(pid1, pid3))

	t.Run("notify with empty result", func(t *testing.T) {
		upstream.reset()
		emptyResult := &runtime.Result{}
		topo.Complete(pid1, emptyResult)

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
		topo.Complete(pid1, result)

		// Verify no notifications are sent
		sends2 := upstream.getSends(pid2)
		assert.Equal(t, 0, len(sends2), "no notifications should be sent for removed process")

		sends3 := upstream.getSends(pid3)
		assert.Equal(t, 0, len(sends3), "no notifications should be sent for removed process")
	})
}

func TestTopology_WatchingMapTracking(t *testing.T) {
	t.Run("Monitor tracks relationship and sends notification on Complete", func(t *testing.T) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")

		caller := pid.PID{Host: "host", UniqID: "caller"}
		caller = caller.Precomputed()
		target := pid.PID{Host: "host", UniqID: "target"}
		target = target.Precomputed()

		_ = topo.Register(caller)
		_ = topo.Register(target)

		err := topo.Monitor(caller, target)
		assert.NoError(t, err)

		// Verify monitoring works by completing target and checking notification
		topo.Complete(target, &runtime.Result{})
		notifications := upstream.getSends(caller)
		assert.Len(t, notifications, 1, "caller should receive notification when target completes")
	})

	t.Run("Demonitor removes relationship so no notification on Complete", func(t *testing.T) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")

		caller := pid.PID{Host: "host", UniqID: "caller"}
		caller = caller.Precomputed()
		target := pid.PID{Host: "host", UniqID: "target"}
		target = target.Precomputed()

		_ = topo.Register(caller)
		_ = topo.Register(target)
		_ = topo.Monitor(caller, target)

		err := topo.Demonitor(caller, target)
		assert.NoError(t, err)

		// Verify demonitor worked - no notification on Complete
		topo.Complete(target, &runtime.Result{})
		notifications := upstream.getSends(caller)
		assert.Len(t, notifications, 0, "caller should not receive notification after demonitor")
	})

	t.Run("Remove cleans up all monitor relationships", func(t *testing.T) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")

		watcher1 := pid.PID{Host: "host", UniqID: "watcher1"}
		watcher1 = watcher1.Precomputed()
		watcher2 := pid.PID{Host: "host", UniqID: "watcher2"}
		watcher2 = watcher2.Precomputed()
		target := pid.PID{Host: "host", UniqID: "target"}
		target = target.Precomputed()

		_ = topo.Register(watcher1)
		_ = topo.Register(watcher2)
		_ = topo.Register(target)

		_ = topo.Monitor(watcher1, target)
		_ = topo.Monitor(watcher2, target)

		// Remove target (without notification)
		topo.Remove(target)

		// Verify watchers' tracking is cleaned by monitoring a new target
		newTarget := pid.PID{Host: "host", UniqID: "newtarget"}
		newTarget = newTarget.Precomputed()
		_ = topo.Register(newTarget)
		_ = topo.Monitor(watcher1, newTarget)

		// Complete new target - should notify watcher1
		topo.Complete(newTarget, &runtime.Result{})
		notifications := upstream.getSends(watcher1)
		assert.Len(t, notifications, 1, "watcher1 should only receive notification from new target")
	})
}

func TestTopology_DuplicateLink(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	// Use "9" and "30" with same host - {host|9} and {host|30} hash to shard 26
	pid1 := pid.PID{Host: "host", UniqID: "9"}
	pid1 = pid1.Precomputed()
	pid2 := pid.PID{Host: "host", UniqID: "30"}
	pid2 = pid2.Precomputed()

	// Register both processes
	assert.NoError(t, topo.Register(pid1))
	assert.NoError(t, topo.Register(pid2))

	// Link first time
	err := topo.Link(pid1, pid2)
	assert.NoError(t, err)

	links := topo.GetLinks(pid1)
	assert.Len(t, links, 1)

	// Link second time - should be no-op (tests linkSameShard duplicate check)
	err = topo.Link(pid1, pid2)
	assert.NoError(t, err)

	// Should still have only one link
	links = topo.GetLinks(pid1)
	assert.Len(t, links, 1, "duplicate link should be no-op")
}

func TestTopology_RecycleStateLargeMaps(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	// Create main process
	mainPID := pid.PID{Host: "host", UniqID: "main"}
	mainPID = mainPID.Precomputed()
	assert.NoError(t, topo.Register(mainPID))

	// Create and register many processes
	watchers := make([]pid.PID, 20)
	linked := make([]pid.PID, 20)
	for i := 0; i < 20; i++ {
		watchers[i] = pid.PID{Host: "watcher", UniqID: string(rune('A' + i))}
		watchers[i] = watchers[i].Precomputed()
		assert.NoError(t, topo.Register(watchers[i]))

		linked[i] = pid.PID{Host: "linked", UniqID: string(rune('a' + i))}
		linked[i] = linked[i].Precomputed()
		assert.NoError(t, topo.Register(linked[i]))
	}

	// Set up 20 monitors and 20 links to main process
	for i := 0; i < 20; i++ {
		assert.NoError(t, topo.Monitor(watchers[i], mainPID))
		assert.NoError(t, topo.Link(mainPID, linked[i]))
	}

	// Also have mainPID watch many targets (to fill watching map)
	targets := make([]pid.PID, 20)
	for i := 0; i < 20; i++ {
		targets[i] = pid.PID{Host: "target", UniqID: string(rune('0' + i))}
		targets[i] = targets[i].Precomputed()
		assert.NoError(t, topo.Register(targets[i]))
		assert.NoError(t, topo.Monitor(mainPID, targets[i]))
	}

	// Complete mainPID - this triggers recycleState with large maps (>16)
	topo.Complete(mainPID, &runtime.Result{})

	// State should be recycled, verify linked processes got cleaned up
	for i := 0; i < 20; i++ {
		links := topo.GetLinks(linked[i])
		assert.Empty(t, links, "linked process should have no links after main completed")
	}
}
