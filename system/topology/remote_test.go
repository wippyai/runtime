// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
)

func TestTopology_RemoteMonitoring(t *testing.T) {
	router := newMockUpstream()
	topo := NewTopology(router, "local")

	localPID := pid.PID{Node: "local", Host: "host1", UniqID: "1"}
	localPID.Precomputed()
	remotePID := pid.PID{Node: "remote", Host: "host2", UniqID: "2"}
	remotePID.Precomputed()

	// Register local caller for remote monitoring tests
	_ = topo.Register(localPID)

	t.Run("Wait on remote node sends MonitorRequest", func(t *testing.T) {
		router.reset()

		err := topo.Monitor(localPID, remotePID)
		require.NoError(t, err)

		pkgs := router.getSends(remotePID)
		require.Len(t, pkgs, 1, "should send MonitorRequest package")

		assert.Equal(t, remotePID, pkgs[0].Target)

		var monitorReq *topology.MonitorRequestEvent
		for _, msg := range pkgs[0].Messages {
			for _, p := range msg.Payloads {
				if req, ok := p.Data().(*topology.MonitorRequestEvent); ok {
					monitorReq = req
					break
				}
			}
		}

		require.NotNil(t, monitorReq, "package should contain MonitorRequestEvent")
		assert.Equal(t, topology.MonitorRequest, monitorReq.Kind)
		assert.Equal(t, localPID, monitorReq.Caller)
		assert.Equal(t, remotePID, monitorReq.Target)
	})

	t.Run("Release on remote node sends MonitorRelease", func(t *testing.T) {
		router.reset()

		err := topo.Demonitor(localPID, remotePID)
		require.NoError(t, err)

		pkgs := router.getSends(remotePID)
		require.Len(t, pkgs, 1, "should send MonitorRelease package")

		var releaseReq *topology.MonitorReleaseEvent
		for _, msg := range pkgs[0].Messages {
			for _, p := range msg.Payloads {
				if req, ok := p.Data().(*topology.MonitorReleaseEvent); ok {
					releaseReq = req
					break
				}
			}
		}

		require.NotNil(t, releaseReq, "package should contain MonitorReleaseEvent")
		assert.Equal(t, topology.MonitorRelease, releaseReq.Kind)
		assert.Equal(t, localPID, releaseReq.Caller)
		assert.Equal(t, remotePID, releaseReq.Target)
	})

	t.Run("Wait on local node does not use router", func(t *testing.T) {
		router.reset()

		// localPID already registered above
		localPID2 := pid.PID{Node: "local", Host: "host2", UniqID: "2"}
		localPID2.Precomputed()
		err := topo.Monitor(localPID2, localPID)
		require.NoError(t, err)

		pkgs := router.getSends(localPID)
		assert.Len(t, pkgs, 0, "should not send package for local monitoring")
	})
}

func TestTopology_RemoteLinking(t *testing.T) {
	router := newMockUpstream()
	topo := NewTopology(router, "local")

	localPID := pid.PID{Node: "local", Host: "host1", UniqID: "1"}
	localPID.Precomputed()
	remotePID := pid.PID{Node: "remote", Host: "host2", UniqID: "2"}
	remotePID.Precomputed()

	err := topo.Register(localPID)
	require.NoError(t, err)

	t.Run("Link to remote node establishes local side and sends LinkRequest", func(t *testing.T) {
		router.reset()

		err := topo.Link(localPID, remotePID)
		require.NoError(t, err)

		links := topo.GetLinks(localPID)
		require.Len(t, links, 1, "should establish local side of link")
		assert.Equal(t, remotePID, links[0])

		pkgs := router.getSends(remotePID)
		require.Len(t, pkgs, 1, "should send LinkRequest package")

		var linkReq *topology.LinkRequestEvent
		for _, msg := range pkgs[0].Messages {
			for _, p := range msg.Payloads {
				if req, ok := p.Data().(*topology.LinkRequestEvent); ok {
					linkReq = req
					break
				}
			}
		}

		require.NotNil(t, linkReq, "package should contain LinkRequestEvent")
		assert.Equal(t, topology.LinkRequest, linkReq.Kind)
		assert.Equal(t, localPID, linkReq.From)
		assert.Equal(t, remotePID, linkReq.To)
	})

	t.Run("Link to same remote node again is idempotent", func(t *testing.T) {
		router.reset()

		err := topo.Link(localPID, remotePID)
		require.NoError(t, err)

		pkgs := router.getSends(remotePID)
		assert.Len(t, pkgs, 0, "should not send duplicate LinkRequest")
	})

	t.Run("Unlink from remote node removes local side and sends UnlinkRequest", func(t *testing.T) {
		router.reset()

		err := topo.Unlink(localPID, remotePID)
		require.NoError(t, err)

		links := topo.GetLinks(localPID)
		assert.Len(t, links, 0, "should remove local side of link")

		pkgs := router.getSends(remotePID)
		require.Len(t, pkgs, 1, "should send UnlinkRequest package")

		var unlinkReq *topology.UnlinkRequestEvent
		for _, msg := range pkgs[0].Messages {
			for _, p := range msg.Payloads {
				if req, ok := p.Data().(*topology.UnlinkRequestEvent); ok {
					unlinkReq = req
					break
				}
			}
		}

		require.NotNil(t, unlinkReq, "package should contain UnlinkRequestEvent")
		assert.Equal(t, topology.UnlinkRequest, unlinkReq.Kind)
		assert.Equal(t, localPID, unlinkReq.From)
		assert.Equal(t, remotePID, unlinkReq.To)
	})

	t.Run("Link to local node does not use router", func(t *testing.T) {
		router.reset()

		localPID2 := pid.PID{Node: "local", Host: "host2", UniqID: "2"}
		localPID2.Precomputed()
		err := topo.Register(localPID2)
		require.NoError(t, err)

		err = topo.Link(localPID, localPID2)
		require.NoError(t, err)

		pkgs := router.getSends(localPID2)
		assert.Len(t, pkgs, 0, "should not send package for local linking")
	})

	t.Run("Link with unregistered from PID fails", func(t *testing.T) {
		unregisteredPID := pid.PID{Node: "local", Host: "host3", UniqID: "3"}
		unregisteredPID.Precomputed()
		remotePID2 := pid.PID{Node: "remote2", Host: "host4", UniqID: "4"}
		remotePID2.Precomputed()

		err := topo.Link(unregisteredPID, remotePID2)
		require.Error(t, err)
		assert.True(t, errors.Is(err, topology.ErrPIDNotRegistered), "expected ErrPIDNotRegistered")
	})
}

func TestTopology_WatcherDeathSendsMonitorRelease(t *testing.T) {
	router := newMockUpstream()
	topo := NewTopology(router, "local")

	localPID := pid.PID{Node: "local", Host: "host1", UniqID: "1"}
	localPID.Precomputed()
	remotePID := pid.PID{Node: "remote", Host: "host2", UniqID: "2"}
	remotePID.Precomputed()

	err := topo.Register(localPID)
	require.NoError(t, err)

	t.Run("Complete sends MonitorRelease to remote targets being watched", func(t *testing.T) {
		// Local process monitors remote process
		err := topo.Monitor(localPID, remotePID)
		require.NoError(t, err)

		router.reset()

		// Local watcher dies
		topo.Complete(localPID, &runtime.Result{})

		// Should send MonitorRelease to remote
		pkgs := router.getSends(remotePID)
		require.Len(t, pkgs, 1, "should send MonitorRelease package to remote target")

		var releaseReq *topology.MonitorReleaseEvent
		for _, msg := range pkgs[0].Messages {
			for _, p := range msg.Payloads {
				if req, ok := p.Data().(*topology.MonitorReleaseEvent); ok {
					releaseReq = req
					break
				}
			}
		}

		require.NotNil(t, releaseReq, "package should contain MonitorReleaseEvent")
		assert.Equal(t, topology.MonitorRelease, releaseReq.Kind)
		assert.Equal(t, localPID, releaseReq.Caller)
		assert.Equal(t, remotePID, releaseReq.Target)
	})

	t.Run("Complete does not send MonitorRelease for local targets", func(t *testing.T) {
		// Setup new local processes
		localPID2 := pid.PID{Node: "local", Host: "host2", UniqID: "2"}
		localPID2.Precomputed()
		localPID3 := pid.PID{Node: "local", Host: "host3", UniqID: "3"}
		localPID3.Precomputed()

		require.NoError(t, topo.Register(localPID2))
		require.NoError(t, topo.Register(localPID3))

		// localPID2 monitors localPID3 (both local)
		err := topo.Monitor(localPID2, localPID3)
		require.NoError(t, err)

		router.reset()

		// localPID2 dies
		topo.Complete(localPID2, &runtime.Result{})

		// Should NOT send MonitorRelease for local target
		pkgs := router.getSends(localPID3)
		assert.Len(t, pkgs, 0, "should not send MonitorRelease for local target")
	})

	t.Run("Complete sends MonitorRelease for multiple remote targets", func(t *testing.T) {
		// Setup
		localPID4 := pid.PID{Node: "local", Host: "host4", UniqID: "4"}
		localPID4.Precomputed()
		remotePID1 := pid.PID{Node: "remote1", Host: "rhost1", UniqID: "r1"}
		remotePID1.Precomputed()
		remotePID2 := pid.PID{Node: "remote2", Host: "rhost2", UniqID: "r2"}
		remotePID2.Precomputed()

		require.NoError(t, topo.Register(localPID4))

		// Monitor two remote targets
		require.NoError(t, topo.Monitor(localPID4, remotePID1))
		require.NoError(t, topo.Monitor(localPID4, remotePID2))

		router.reset()

		// Local watcher dies
		topo.Complete(localPID4, &runtime.Result{})

		// Should send MonitorRelease to both remotes
		pkgs1 := router.getSends(remotePID1)
		pkgs2 := router.getSends(remotePID2)
		assert.Len(t, pkgs1, 1, "should send MonitorRelease to first remote")
		assert.Len(t, pkgs2, 1, "should send MonitorRelease to second remote")
	})
}

func TestTopology_handleMonitorRequest(t *testing.T) {
	t.Run("handleMonitorRequest adds caller to watchers", func(t *testing.T) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")

		localPID := pid.PID{Node: "local", Host: "host1", UniqID: "1"}
		localPID.Precomputed()
		remotePID := pid.PID{Node: "remote", Host: "host2", UniqID: "2"}
		remotePID.Precomputed()

		err := topo.Register(localPID)
		require.NoError(t, err)

		err = topo.handleMonitorRequest(remotePID, localPID)
		require.NoError(t, err)

		// Verify watcher was added by checking notification on Complete
		topo.Complete(localPID, &runtime.Result{})
		assert.Len(t, upstream.getSends(remotePID), 1, "remotePID should receive notification")
	})

	t.Run("handleMonitorRequest on unregistered PID fails", func(t *testing.T) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")

		remotePID := pid.PID{Node: "remote", Host: "host2", UniqID: "2"}
		remotePID.Precomputed()
		unregisteredPID := pid.PID{Node: "local", Host: "host3", UniqID: "3"}
		unregisteredPID.Precomputed()

		err := topo.handleMonitorRequest(remotePID, unregisteredPID)
		require.Error(t, err)
		assert.True(t, errors.Is(err, topology.ErrPIDNotRegistered), "expected ErrPIDNotRegistered")
	})

	t.Run("handleMonitorRequest is idempotent", func(t *testing.T) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")

		localPID := pid.PID{Node: "local", Host: "host1", UniqID: "1"}
		localPID.Precomputed()
		remotePID := pid.PID{Node: "remote", Host: "host2", UniqID: "2"}
		remotePID.Precomputed()

		require.NoError(t, topo.Register(localPID))
		require.NoError(t, topo.handleMonitorRequest(remotePID, localPID))
		require.NoError(t, topo.handleMonitorRequest(remotePID, localPID)) // add again

		// Verify only one notification is sent (not duplicated)
		topo.Complete(localPID, &runtime.Result{})
		assert.Len(t, upstream.getSends(remotePID), 1, "should not add duplicate watchers")
	})
}

func TestTopology_handleMonitorRelease(t *testing.T) {
	t.Run("handleMonitorRelease removes caller from watchers", func(t *testing.T) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")

		localPID := pid.PID{Node: "local", Host: "host1", UniqID: "1"}
		localPID.Precomputed()
		remotePID := pid.PID{Node: "remote", Host: "host2", UniqID: "2"}
		remotePID.Precomputed()

		require.NoError(t, topo.Register(localPID))
		require.NoError(t, topo.handleMonitorRequest(remotePID, localPID))

		err := topo.handleMonitorRelease(remotePID, localPID)
		require.NoError(t, err)

		// Verify watcher was removed - no notification on Complete
		topo.Complete(localPID, &runtime.Result{})
		assert.Len(t, upstream.getSends(remotePID), 0, "should have no watchers after release")
	})

	t.Run("handleMonitorRelease on non-monitored PID is safe", func(t *testing.T) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")

		remotePID := pid.PID{Node: "remote", Host: "host2", UniqID: "2"}
		remotePID.Precomputed()
		unmonitoredPID := pid.PID{Node: "local", Host: "host3", UniqID: "3"}
		unmonitoredPID.Precomputed()

		err := topo.handleMonitorRelease(remotePID, unmonitoredPID)
		require.NoError(t, err)
	})
}

func TestTopology_handleLinkRequest(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	localPID := pid.PID{Node: "local", Host: "host1", UniqID: "1"}
	localPID.Precomputed()
	remotePID := pid.PID{Node: "remote", Host: "host2", UniqID: "2"}
	remotePID.Precomputed()

	err := topo.Register(localPID)
	require.NoError(t, err)

	t.Run("handleLinkRequest establishes remote side of link", func(t *testing.T) {
		err := topo.handleLinkRequest(remotePID, localPID)
		require.NoError(t, err)

		links := topo.GetLinks(localPID)
		require.Len(t, links, 1, "should establish link")
		assert.Equal(t, remotePID, links[0])
	})

	t.Run("handleLinkRequest on unregistered to PID fails", func(t *testing.T) {
		unregisteredPID := pid.PID{Node: "local", Host: "host3", UniqID: "3"}
		unregisteredPID.Precomputed()

		err := topo.handleLinkRequest(remotePID, unregisteredPID)
		require.Error(t, err)
		assert.True(t, errors.Is(err, topology.ErrPIDNotRegistered), "expected ErrPIDNotRegistered")
	})

	t.Run("handleLinkRequest is idempotent", func(t *testing.T) {
		err := topo.handleLinkRequest(remotePID, localPID)
		require.NoError(t, err)

		links := topo.GetLinks(localPID)
		assert.Len(t, links, 1, "should not create duplicate links")
	})
}

func TestTopology_handleUnlinkRequest(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	localPID := pid.PID{Node: "local", Host: "host1", UniqID: "1"}
	localPID.Precomputed()
	remotePID := pid.PID{Node: "remote", Host: "host2", UniqID: "2"}
	remotePID.Precomputed()

	err := topo.Register(localPID)
	require.NoError(t, err)

	err = topo.handleLinkRequest(remotePID, localPID)
	require.NoError(t, err)

	t.Run("handleUnlinkRequest removes link", func(t *testing.T) {
		err := topo.handleUnlinkRequest(remotePID, localPID)
		require.NoError(t, err)

		links := topo.GetLinks(localPID)
		assert.Len(t, links, 0, "should remove link")
	})

	t.Run("handleUnlinkRequest on non-linked PID is safe", func(t *testing.T) {
		err := topo.handleUnlinkRequest(remotePID, localPID)
		require.NoError(t, err)
	})
}

func TestTopology_RemoteMonitoringWithNotification(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	localPID := pid.PID{Node: "local", Host: "host1", UniqID: "1"}
	localPID.Precomputed()
	remotePID := pid.PID{Node: "remote", Host: "host2", UniqID: "2"}
	remotePID.Precomputed()

	t.Run("Remote watcher receives exit notification", func(t *testing.T) {
		err := topo.Register(localPID)
		require.NoError(t, err)

		err = topo.handleMonitorRequest(remotePID, localPID)
		require.NoError(t, err)

		upstream.reset()

		topo.Complete(localPID, &runtime.Result{
			Value: payload.New("test result"),
			Error: nil,
		})

		pkgs := upstream.getSends(remotePID)
		require.Len(t, pkgs, 1, "should send exit notification to remote watcher")

		var exitEvent *topology.ExitEvent
		for _, msg := range pkgs[0].Messages {
			for _, p := range msg.Payloads {
				if evt, ok := p.Data().(*topology.ExitEvent); ok {
					exitEvent = evt
					break
				}
			}
		}

		require.NotNil(t, exitEvent, "should contain ExitEvent")
		assert.Equal(t, topology.Exit, exitEvent.Kind)
		assert.Equal(t, localPID, exitEvent.From)
	})
}

func TestTopology_HandleNodeExit(t *testing.T) {
	router := newMockUpstream()
	topo := NewTopology(router, "local")

	localPID1 := pid.PID{Node: "local", Host: "host1", UniqID: "p1"}
	localPID1.Precomputed()
	localPID2 := pid.PID{Node: "local", Host: "host2", UniqID: "p2"}
	localPID2.Precomputed()
	remotePID1 := pid.PID{Node: "remote", Host: "host1", UniqID: "r1"}
	remotePID1.Precomputed()
	remotePID2 := pid.PID{Node: "remote", Host: "host2", UniqID: "r2"}
	remotePID2.Precomputed()
	otherRemotePID := pid.PID{Node: "other", Host: "host1", UniqID: "o1"}
	otherRemotePID.Precomputed()

	// Register local processes
	require.NoError(t, topo.Register(localPID1))
	require.NoError(t, topo.Register(localPID2))

	t.Run("HandleNodeExit notifies processes watching remote PIDs", func(t *testing.T) {
		router.reset()

		// Local process watches remote PID
		err := topo.Monitor(localPID1, remotePID1)
		require.NoError(t, err)

		router.reset() // Clear the MonitorRequest

		// Simulate node exit
		topo.HandleNodeExit("remote", errors.New("node disconnected"))

		pkgs := router.getSends(localPID1)
		require.Len(t, pkgs, 1, "should send LinkDown to local watcher")

		var exitEvent *topology.ExitEvent
		for _, msg := range pkgs[0].Messages {
			for _, p := range msg.Payloads {
				if evt, ok := p.Data().(*topology.ExitEvent); ok {
					exitEvent = evt
					break
				}
			}
		}

		require.NotNil(t, exitEvent)
		assert.Equal(t, topology.LinkDown, exitEvent.Kind)
		assert.Equal(t, remotePID1, exitEvent.From)
	})

	t.Run("HandleNodeExit notifies processes linked to remote PIDs", func(t *testing.T) {
		router.reset()

		// localPID1 still registered (HandleNodeExit only removes remote PIDs)
		// Local process links to remote PID
		err := topo.Link(localPID1, remotePID2)
		require.NoError(t, err)

		router.reset() // Clear the LinkRequest

		// Simulate node exit
		topo.HandleNodeExit("remote", errors.New("node crashed"))

		pkgs := router.getSends(localPID1)
		require.Len(t, pkgs, 1, "should send LinkDown to linked process")
	})

	t.Run("HandleNodeExit does not affect other nodes", func(t *testing.T) {
		router.reset()

		// localPID2 still registered (HandleNodeExit only removes remote PIDs)
		// Watch a PID on a different node
		err := topo.Monitor(localPID2, otherRemotePID)
		require.NoError(t, err)

		router.reset()

		// Exit different node
		topo.HandleNodeExit("remote", errors.New("node gone"))

		// Should not notify about "other" node
		pkgs := router.getSends(localPID2)
		assert.Len(t, pkgs, 0, "should not notify about different node")
	})

	t.Run("HandleNodeExit cleans up watching entries", func(t *testing.T) {
		router.reset()

		// localPID1 still registered
		// Watch remote PID
		remotePID := pid.PID{Node: "cleanup-test", Host: "h", UniqID: "r"}
		remotePID.Precomputed()
		err := topo.Monitor(localPID1, remotePID)
		require.NoError(t, err)

		router.reset()

		// Handle node exit
		topo.HandleNodeExit("cleanup-test", errors.New("cleanup"))

		// Calling again should not send anything (already cleaned)
		router.reset()
		topo.HandleNodeExit("cleanup-test", errors.New("second"))

		pkgs := router.getSends(localPID1)
		assert.Len(t, pkgs, 0, "should not notify again after cleanup")
	})

	t.Run("HandleNodeExit removes registered remote PIDs from topology", func(t *testing.T) {
		router.reset()
		topo2 := NewTopology(router, "local")

		// Register a "remote" PID (simulating a remote process registered via handleMonitorRequest)
		remotePID := pid.PID{Node: "dying-node", Host: "h", UniqID: "r1"}
		remotePID.Precomputed()
		err := topo2.Register(remotePID)
		require.NoError(t, err)

		// Register local process to watch it
		localPID := pid.PID{Node: "local", Host: "h", UniqID: "l1"}
		localPID.Precomputed()
		err = topo2.Register(localPID)
		require.NoError(t, err)

		err = topo2.Monitor(localPID, remotePID)
		require.NoError(t, err)

		router.reset()

		// Handle node exit - should remove remotePID and notify localPID
		topo2.HandleNodeExit("dying-node", errors.New("node died"))

		// Remote PID should be removed - can re-register it
		err = topo2.Register(remotePID)
		assert.NoError(t, err, "remote PID should be removed, re-registration should succeed")

		// Local should still be registered - cannot re-register
		err = topo2.Register(localPID)
		assert.Error(t, err, "local PID should remain, re-registration should fail")

		// Local should be notified
		pkgs := router.getSends(localPID)
		assert.Len(t, pkgs, 1, "should notify local watcher")
	})

	t.Run("HandleNodeExit cleans up links in remaining processes", func(t *testing.T) {
		router.reset()
		topo2 := NewTopology(router, "local")

		localPID := pid.PID{Node: "local", Host: "h", UniqID: "l1"}
		localPID.Precomputed()
		remotePID := pid.PID{Node: "remote-node", Host: "h", UniqID: "r1"}
		remotePID.Precomputed()

		err := topo2.Register(localPID)
		require.NoError(t, err)

		// Link to remote
		err = topo2.Link(localPID, remotePID)
		require.NoError(t, err)

		// Verify link exists
		links := topo2.GetLinks(localPID)
		assert.Len(t, links, 1, "should have link")

		router.reset()

		// Handle node exit
		topo2.HandleNodeExit("remote-node", errors.New("died"))

		// Link should be cleaned up
		links = topo2.GetLinks(localPID)
		assert.Len(t, links, 0, "link should be cleaned up")
	})

	t.Run("HandleNodeExit cleans up watchers when remote watcher dies", func(t *testing.T) {
		router.reset()
		topo2 := NewTopology(router, "local")

		localPID := pid.PID{Node: "local", Host: "h", UniqID: "l1"}
		localPID.Precomputed()
		remotePID := pid.PID{Node: "remote-watcher", Host: "h", UniqID: "r1"}
		remotePID.Precomputed()

		err := topo2.Register(localPID)
		require.NoError(t, err)

		// Remote is watching local (via handleMonitorRequest)
		err = topo2.handleMonitorRequest(remotePID, localPID)
		require.NoError(t, err)

		router.reset()

		// Remote node dies
		topo2.HandleNodeExit("remote-watcher", errors.New("died"))

		// Complete local - should not try to notify dead remote
		topo2.Complete(localPID, &runtime.Result{})

		// Should not have sent to remote (it's dead and cleaned up)
		pkgs := router.getSends(remotePID)
		assert.Len(t, pkgs, 0, "should not notify dead remote watcher")
	})

	t.Run("HandleNodeExit with multiple processes watching same remote", func(t *testing.T) {
		router.reset()
		topo2 := NewTopology(router, "local")

		local1 := pid.PID{Node: "local", Host: "h1", UniqID: "l1"}
		local1.Precomputed()
		local2 := pid.PID{Node: "local", Host: "h2", UniqID: "l2"}
		local2.Precomputed()
		local3 := pid.PID{Node: "local", Host: "h3", UniqID: "l3"}
		local3.Precomputed()
		remotePID := pid.PID{Node: "multi-watch-node", Host: "h", UniqID: "r1"}
		remotePID.Precomputed()

		require.NoError(t, topo2.Register(local1))
		require.NoError(t, topo2.Register(local2))
		require.NoError(t, topo2.Register(local3))

		// All three watch remote
		require.NoError(t, topo2.Monitor(local1, remotePID))
		require.NoError(t, topo2.Monitor(local2, remotePID))
		require.NoError(t, topo2.Monitor(local3, remotePID))

		router.reset()

		// Remote node dies
		topo2.HandleNodeExit("multi-watch-node", errors.New("died"))

		// All three should be notified
		assert.Len(t, router.getSends(local1), 1)
		assert.Len(t, router.getSends(local2), 1)
		assert.Len(t, router.getSends(local3), 1)
	})

	t.Run("HandleNodeExit with empty node is no-op", func(t *testing.T) {
		router.reset()
		topo2 := NewTopology(router, "local")

		// Just call with unknown node - should not panic
		topo2.HandleNodeExit("unknown-node", errors.New("died"))

		// No packages sent
		assert.Len(t, router.sends, 0)
	})
}
