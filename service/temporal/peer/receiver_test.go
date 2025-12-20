package peer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

type mockRouter struct {
	packages []*relay.Package
}

func (m *mockRouter) Send(pkg *relay.Package) error {
	m.packages = append(m.packages, pkg)
	return nil
}

func TestReceiver_HandleExitEvent(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}

	t.Run("ExitEvent removes linked process from watcher", func(t *testing.T) {
		r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

		workflowPID := pid.PID{Node: "temporal-client", Host: "task-queue", UniqID: "workflow-123"}
		workflowPID.Precomputed()
		localPID := pid.PID{Node: "local", Host: "host1", UniqID: "process-1"}
		localPID.Precomputed()

		// Directly set up watcher state (to avoid starting watchWorkflow goroutine)
		r.mu.Lock()
		r.watchers[workflowPID.UniqID] = &workflowWatcher{
			workflowID: workflowPID.UniqID,
			taskQueue:  workflowPID.Host,
			monitors:   make(map[string]pid.PID),
			links:      map[string]pid.PID{localPID.String(): localPID},
		}
		r.mu.Unlock()

		// Verify link was added
		r.mu.RLock()
		watcher, exists := r.watchers[workflowPID.UniqID]
		require.True(t, exists)
		assert.Len(t, watcher.links, 1)
		r.mu.RUnlock()

		// Local process dies - sends ExitEvent
		exitEvent := &topology.ExitEvent{
			Kind: topology.LinkDown,
			From: localPID,
		}
		exitPkg := relay.NewPackage(localPID, workflowPID, topology.TopicEvents, payload.New(exitEvent))
		err := r.Send(exitPkg)
		require.NoError(t, err)

		// Verify link was removed
		r.mu.RLock()
		_, exists = r.watchers[workflowPID.UniqID]
		// Watcher should be cleaned up since no links/monitors remain
		assert.False(t, exists, "watcher should be cleaned up when empty")
		r.mu.RUnlock()
	})

	t.Run("ExitEvent removes monitoring process from watcher", func(t *testing.T) {
		r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

		workflowPID := pid.PID{Node: "temporal-client", Host: "task-queue", UniqID: "workflow-456"}
		workflowPID.Precomputed()
		localPID := pid.PID{Node: "local", Host: "host1", UniqID: "process-2"}
		localPID.Precomputed()

		// Directly set up watcher state (to avoid starting watchWorkflow goroutine)
		r.mu.Lock()
		r.watchers[workflowPID.UniqID] = &workflowWatcher{
			workflowID: workflowPID.UniqID,
			taskQueue:  workflowPID.Host,
			monitors:   map[string]pid.PID{localPID.String(): localPID},
			links:      make(map[string]pid.PID),
		}
		r.mu.Unlock()

		// Verify monitor was added
		r.mu.RLock()
		watcher, exists := r.watchers[workflowPID.UniqID]
		require.True(t, exists)
		assert.Len(t, watcher.monitors, 1)
		r.mu.RUnlock()

		// Local process dies - sends ExitEvent
		exitEvent := &topology.ExitEvent{
			Kind: topology.Exit,
			From: localPID,
		}
		exitPkg := relay.NewPackage(localPID, workflowPID, topology.TopicEvents, payload.New(exitEvent))
		err := r.Send(exitPkg)
		require.NoError(t, err)

		// Verify monitor was removed
		r.mu.RLock()
		_, exists = r.watchers[workflowPID.UniqID]
		// Watcher should be cleaned up since no links/monitors remain
		assert.False(t, exists, "watcher should be cleaned up when empty")
		r.mu.RUnlock()
	})

	t.Run("ExitEvent with multiple watchers only removes sender", func(t *testing.T) {
		r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

		workflowPID := pid.PID{Node: "temporal-client", Host: "task-queue", UniqID: "workflow-789"}
		workflowPID.Precomputed()
		localPID1 := pid.PID{Node: "local", Host: "host1", UniqID: "process-1"}
		localPID1.Precomputed()
		localPID2 := pid.PID{Node: "local", Host: "host2", UniqID: "process-2"}
		localPID2.Precomputed()

		// Directly set up watcher with both links (to avoid starting watchWorkflow goroutine)
		r.mu.Lock()
		r.watchers[workflowPID.UniqID] = &workflowWatcher{
			workflowID: workflowPID.UniqID,
			taskQueue:  workflowPID.Host,
			monitors:   make(map[string]pid.PID),
			links: map[string]pid.PID{
				localPID1.String(): localPID1,
				localPID2.String(): localPID2,
			},
		}
		r.mu.Unlock()

		// Verify both links exist
		r.mu.RLock()
		watcher := r.watchers[workflowPID.UniqID]
		assert.Len(t, watcher.links, 2)
		r.mu.RUnlock()

		// First process dies
		exitEvent := &topology.ExitEvent{Kind: topology.LinkDown, From: localPID1}
		require.NoError(t, r.Send(relay.NewPackage(localPID1, workflowPID, topology.TopicEvents, payload.New(exitEvent))))

		// Verify only first link was removed
		r.mu.RLock()
		watcher, exists := r.watchers[workflowPID.UniqID]
		require.True(t, exists, "watcher should still exist")
		assert.Len(t, watcher.links, 1)
		_, hasLink2 := watcher.links[localPID2.String()]
		assert.True(t, hasLink2, "second link should still exist")
		r.mu.RUnlock()
	})

	t.Run("ExitEvent for unknown workflow is no-op", func(t *testing.T) {
		r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

		workflowPID := pid.PID{Node: "temporal-client", Host: "task-queue", UniqID: "unknown-workflow"}
		workflowPID.Precomputed()
		localPID := pid.PID{Node: "local", Host: "host1", UniqID: "process-1"}
		localPID.Precomputed()

		// Send exit for unknown workflow
		exitEvent := &topology.ExitEvent{Kind: topology.LinkDown, From: localPID}
		err := r.Send(relay.NewPackage(localPID, workflowPID, topology.TopicEvents, payload.New(exitEvent)))
		require.NoError(t, err, "should not error for unknown workflow")
	})
}

func TestReceiver_HandleMonitorRelease(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}

	t.Run("MonitorRelease removes monitor from watcher", func(t *testing.T) {
		r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

		workflowPID := pid.PID{Node: "temporal-client", Host: "task-queue", UniqID: "workflow-123"}
		workflowPID.Precomputed()
		localPID := pid.PID{Node: "local", Host: "host1", UniqID: "process-1"}
		localPID.Precomputed()

		// Directly set up watcher state (to avoid starting watchWorkflow goroutine)
		r.mu.Lock()
		r.watchers[workflowPID.UniqID] = &workflowWatcher{
			workflowID: workflowPID.UniqID,
			taskQueue:  workflowPID.Host,
			monitors:   map[string]pid.PID{localPID.String(): localPID},
			links:      make(map[string]pid.PID),
		}
		r.mu.Unlock()

		// Verify monitor was added
		r.mu.RLock()
		watcher := r.watchers[workflowPID.UniqID]
		assert.Len(t, watcher.monitors, 1)
		r.mu.RUnlock()

		// Release monitor
		releaseReq := &topology.MonitorReleaseEvent{Kind: topology.MonitorRelease, Caller: localPID, Target: workflowPID}
		require.NoError(t, r.Send(relay.NewPackage(localPID, workflowPID, topology.TopicEvents, payload.New(releaseReq))))

		// Verify monitor was removed and watcher cleaned up
		r.mu.RLock()
		_, exists := r.watchers[workflowPID.UniqID]
		assert.False(t, exists, "watcher should be cleaned up when empty")
		r.mu.RUnlock()
	})

	t.Run("MonitorRelease for unknown workflow is no-op", func(t *testing.T) {
		r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

		workflowPID := pid.PID{Node: "temporal-client", Host: "task-queue", UniqID: "unknown"}
		localPID := pid.PID{Node: "local", Host: "host1", UniqID: "process-1"}

		releaseReq := &topology.MonitorReleaseEvent{Caller: localPID, Target: workflowPID}
		err := r.Send(relay.NewPackage(localPID, workflowPID, topology.TopicEvents, payload.New(releaseReq)))
		require.NoError(t, err)
	})
}

func TestReceiver_HandleUnlinkRequest(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}

	t.Run("UnlinkRequest removes link from watcher", func(t *testing.T) {
		r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

		workflowPID := pid.PID{Node: "temporal-client", Host: "task-queue", UniqID: "workflow-123"}
		workflowPID.Precomputed()
		localPID := pid.PID{Node: "local", Host: "host1", UniqID: "process-1"}
		localPID.Precomputed()

		// Set up watcher with link
		r.mu.Lock()
		r.watchers[workflowPID.UniqID] = &workflowWatcher{
			workflowID: workflowPID.UniqID,
			taskQueue:  workflowPID.Host,
			monitors:   make(map[string]pid.PID),
			links:      map[string]pid.PID{localPID.String(): localPID},
		}
		r.mu.Unlock()

		// Unlink request
		unlinkReq := &topology.UnlinkRequestEvent{From: localPID, To: workflowPID}
		require.NoError(t, r.Send(relay.NewPackage(localPID, workflowPID, topology.TopicEvents, payload.New(unlinkReq))))

		// Verify watcher cleaned up
		r.mu.RLock()
		_, exists := r.watchers[workflowPID.UniqID]
		assert.False(t, exists)
		r.mu.RUnlock()
	})

	t.Run("UnlinkRequest for unknown workflow is no-op", func(t *testing.T) {
		r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

		workflowPID := pid.PID{UniqID: "unknown"}
		localPID := pid.PID{UniqID: "process-1"}

		unlinkReq := &topology.UnlinkRequestEvent{From: localPID, To: workflowPID}
		err := r.Send(relay.NewPackage(localPID, workflowPID, topology.TopicEvents, payload.New(unlinkReq)))
		require.NoError(t, err)
	})
}

func TestReceiver_Stop(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}

	r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

	// Set up some watchers
	r.mu.Lock()
	r.watchers["wf1"] = &workflowWatcher{workflowID: "wf1", monitors: make(map[string]pid.PID), links: make(map[string]pid.PID)}
	r.watchers["wf2"] = &workflowWatcher{workflowID: "wf2", monitors: make(map[string]pid.PID), links: make(map[string]pid.PID)}
	r.mu.Unlock()

	r.Stop()

	r.mu.RLock()
	assert.Empty(t, r.watchers)
	r.mu.RUnlock()
}

func TestNewReceiver(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}

	r := NewReceiver(context.Background(), "node1", nil, router, logger)
	require.NotNil(t, r)
	assert.Equal(t, "node1", r.nodeID)
	assert.NotNil(t, r.watchers)
	assert.NotNil(t, r.ctx)
}

func TestReceiver_SendEmptyPackage(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}

	r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

	// Empty package
	err := r.Send(&relay.Package{Messages: nil})
	require.NoError(t, err)

	// Package with empty messages
	err = r.Send(&relay.Package{Messages: []*relay.Message{}})
	require.NoError(t, err)

	// Package with message but no payloads
	err = r.Send(&relay.Package{Messages: []*relay.Message{{Topic: "test", Payloads: nil}}})
	require.NoError(t, err)
}

func TestReceiver_HandleLinkRequest(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}

	t.Run("LinkRequest adds to existing watcher", func(t *testing.T) {
		r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

		workflowPID := pid.PID{Node: "temporal-client", Host: "task-queue", UniqID: "workflow-link-2"}
		localPID1 := pid.PID{Node: "local", Host: "host1", UniqID: "process-1"}
		localPID1.Precomputed()
		localPID2 := pid.PID{Node: "local", Host: "host2", UniqID: "process-2"}
		localPID2.Precomputed()

		// Pre-create watcher (already watching, no goroutine will be started)
		r.mu.Lock()
		r.watchers[workflowPID.UniqID] = &workflowWatcher{
			workflowID: workflowPID.UniqID,
			taskQueue:  workflowPID.Host,
			monitors:   make(map[string]pid.PID),
			links:      map[string]pid.PID{localPID1.String(): localPID1},
			watching:   true,
		}
		r.mu.Unlock()

		// Add second link - won't start goroutine since already watching
		linkReq := &topology.LinkRequestEvent{From: localPID2, To: workflowPID}
		err := r.Send(relay.NewPackage(localPID2, workflowPID, topology.TopicEvents, payload.New(linkReq)))
		require.NoError(t, err)

		// Verify second link was added
		r.mu.RLock()
		watcher := r.watchers[workflowPID.UniqID]
		assert.Len(t, watcher.links, 2)
		r.mu.RUnlock()
	})
}

func TestReceiver_NotifyCompletion(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}

	t.Run("notifies monitors on success", func(t *testing.T) {
		r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

		monitorPID := pid.PID{Node: "local", Host: "host1", UniqID: "monitor-1"}
		monitorPID.Precomputed()

		watcher := &workflowWatcher{
			workflowID: "workflow-success",
			taskQueue:  "task-queue",
			monitors:   map[string]pid.PID{monitorPID.String(): monitorPID},
			links:      make(map[string]pid.PID),
		}
		r.watchers[watcher.workflowID] = watcher

		r.notifyCompletion(watcher, "success-result", nil)

		// Verify EXIT was sent to monitor
		require.Len(t, router.packages, 1)
		pkg := router.packages[0]
		assert.Equal(t, monitorPID.String(), pkg.Target.String())
		assert.Len(t, pkg.Messages, 1)
		assert.Equal(t, topology.TopicEvents, pkg.Messages[0].Topic)
	})

	t.Run("notifies links on error with LINK_DOWN", func(t *testing.T) {
		router := &mockRouter{}
		r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

		linkPID := pid.PID{Node: "local", Host: "host1", UniqID: "link-1"}
		linkPID.Precomputed()

		watcher := &workflowWatcher{
			workflowID: "workflow-error",
			taskQueue:  "task-queue",
			monitors:   make(map[string]pid.PID),
			links:      map[string]pid.PID{linkPID.String(): linkPID},
		}
		r.watchers[watcher.workflowID] = watcher

		r.notifyCompletion(watcher, nil, assert.AnError)

		// Verify LINK_DOWN was sent
		require.Len(t, router.packages, 1)
		pkg := router.packages[0]
		assert.Equal(t, linkPID.String(), pkg.Target.String())

		exitEvent := pkg.Messages[0].Payloads[0].Data().(*topology.ExitEvent)
		assert.Equal(t, topology.LinkDown, exitEvent.Kind)
	})

	t.Run("notifies links on success with EXIT", func(t *testing.T) {
		router := &mockRouter{}
		r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

		linkPID := pid.PID{Node: "local", Host: "host1", UniqID: "link-2"}
		linkPID.Precomputed()

		watcher := &workflowWatcher{
			workflowID: "workflow-success-link",
			taskQueue:  "task-queue",
			monitors:   make(map[string]pid.PID),
			links:      map[string]pid.PID{linkPID.String(): linkPID},
		}
		r.watchers[watcher.workflowID] = watcher

		r.notifyCompletion(watcher, "result", nil)

		// Verify EXIT was sent (not LINK_DOWN)
		require.Len(t, router.packages, 1)
		exitEvent := router.packages[0].Messages[0].Payloads[0].Data().(*topology.ExitEvent)
		assert.Equal(t, topology.Exit, exitEvent.Kind)
	})
}

func TestReceiver_CleanupWatcherWithCancel(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}

	r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

	canceled := false
	watcher := &workflowWatcher{
		workflowID: "workflow-cancel",
		taskQueue:  "task-queue",
		monitors:   make(map[string]pid.PID),
		links:      make(map[string]pid.PID),
		cancel:     func() { canceled = true },
	}
	r.watchers[watcher.workflowID] = watcher

	r.cleanupWatcherIfEmpty(watcher)

	assert.True(t, canceled, "cancel function should be called")
	assert.Empty(t, r.watchers)
}

func TestReceiver_StopWithCancelFunctions(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}

	r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

	canceled1, canceled2 := false, false
	r.mu.Lock()
	r.watchers["wf1"] = &workflowWatcher{
		workflowID: "wf1",
		monitors:   make(map[string]pid.PID),
		links:      make(map[string]pid.PID),
		cancel:     func() { canceled1 = true },
	}
	r.watchers["wf2"] = &workflowWatcher{
		workflowID: "wf2",
		monitors:   make(map[string]pid.PID),
		links:      make(map[string]pid.PID),
		cancel:     func() { canceled2 = true },
	}
	r.mu.Unlock()

	r.Stop()

	assert.True(t, canceled1, "first watcher cancel should be called")
	assert.True(t, canceled2, "second watcher cancel should be called")
	assert.Empty(t, r.watchers)
}

func TestReceiver_SendUnknownPayload(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}

	r := NewReceiver(context.Background(), "temporal-client", nil, router, logger)

	// Send package with unknown payload type
	unknownPayload := payload.NewPayload("unknown", payload.String)
	pkg := &relay.Package{
		Messages: []*relay.Message{{
			Topic:    "test",
			Payloads: []payload.Payload{unknownPayload},
		}},
	}

	err := r.Send(pkg)
	require.NoError(t, err, "unknown payload should be ignored")
}
