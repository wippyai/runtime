package topology

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
)

func TestTopology_RemoteMonitoring(t *testing.T) {
	upstream := newMockUpstream()
	router := newMockUpstream()
	topo := NewTopology(upstream, router, "local")

	localPID := relay.PID{Node: "local", Host: "host1", UniqID: "1"}.Precomputed()
	remotePID := relay.PID{Node: "remote", Host: "host2", UniqID: "2"}.Precomputed()

	t.Run("Wait on remote node sends MonitorRequest", func(t *testing.T) {
		router.reset()

		err := topo.Wait(localPID, remotePID)
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
		assert.Equal(t, topology.KindMonitorRequest, monitorReq.Kind)
		assert.Equal(t, localPID, monitorReq.Caller)
		assert.Equal(t, remotePID, monitorReq.Target)
	})

	t.Run("Release on remote node sends MonitorRelease", func(t *testing.T) {
		router.reset()

		err := topo.Release(localPID, remotePID)
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
		assert.Equal(t, topology.KindMonitorRelease, releaseReq.Kind)
		assert.Equal(t, localPID, releaseReq.Caller)
		assert.Equal(t, remotePID, releaseReq.Target)
	})

	t.Run("Wait on local node does not use router", func(t *testing.T) {
		router.reset()

		err := topo.Register(localPID)
		require.NoError(t, err)

		localPID2 := relay.PID{Node: "local", Host: "host2", UniqID: "2"}.Precomputed()
		err = topo.Wait(localPID2, localPID)
		require.NoError(t, err)

		pkgs := router.getSends(localPID)
		assert.Len(t, pkgs, 0, "should not send package for local monitoring")
	})
}

func TestTopology_RemoteLinking(t *testing.T) {
	upstream := newMockUpstream()
	router := newMockUpstream()
	topo := NewTopology(upstream, router, "local")

	localPID := relay.PID{Node: "local", Host: "host1", UniqID: "1"}.Precomputed()
	remotePID := relay.PID{Node: "remote", Host: "host2", UniqID: "2"}.Precomputed()

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
		assert.Equal(t, topology.KindLinkRequest, linkReq.Kind)
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
		assert.Equal(t, topology.KindUnlinkRequest, unlinkReq.Kind)
		assert.Equal(t, localPID, unlinkReq.From)
		assert.Equal(t, remotePID, unlinkReq.To)
	})

	t.Run("Link to local node does not use router", func(t *testing.T) {
		router.reset()

		localPID2 := relay.PID{Node: "local", Host: "host2", UniqID: "2"}.Precomputed()
		err := topo.Register(localPID2)
		require.NoError(t, err)

		err = topo.Link(localPID, localPID2)
		require.NoError(t, err)

		pkgs := router.getSends(localPID2)
		assert.Len(t, pkgs, 0, "should not send package for local linking")
	})

	t.Run("Link with unregistered from PID fails", func(t *testing.T) {
		unregisteredPID := relay.PID{Node: "local", Host: "host3", UniqID: "3"}.Precomputed()
		remotePID2 := relay.PID{Node: "remote2", Host: "host4", UniqID: "4"}.Precomputed()

		err := topo.Link(unregisteredPID, remotePID2)
		require.Error(t, err)
		assert.True(t, errors.Is(err, topology.ErrPIDNotRegistered), "expected ErrPIDNotRegistered")
	})
}

func TestTopology_HandleMonitorRequest(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, upstream, "local")

	localPID := relay.PID{Node: "local", Host: "host1", UniqID: "1"}.Precomputed()
	remotePID := relay.PID{Node: "remote", Host: "host2", UniqID: "2"}.Precomputed()

	t.Run("HandleMonitorRequest adds caller to watchers", func(t *testing.T) {
		err := topo.Register(localPID)
		require.NoError(t, err)

		err = topo.HandleMonitorRequest(remotePID, localPID)
		require.NoError(t, err)

		value, ok := topo.monitors.Load(localPID.String())
		require.True(t, ok, "should have monitors for localPID")

		watchers := value.(*sync.Map)
		_, ok = watchers.Load(remotePID.String())
		assert.True(t, ok, "remotePID should be in watchers")
	})

	t.Run("HandleMonitorRequest on unregistered PID fails", func(t *testing.T) {
		unregisteredPID := relay.PID{Node: "local", Host: "host3", UniqID: "3"}.Precomputed()

		err := topo.HandleMonitorRequest(remotePID, unregisteredPID)
		require.Error(t, err)
		assert.True(t, errors.Is(err, topology.ErrPIDNotRegistered), "expected ErrPIDNotRegistered")
	})

	t.Run("HandleMonitorRequest is idempotent", func(t *testing.T) {
		err := topo.HandleMonitorRequest(remotePID, localPID)
		require.NoError(t, err)

		value, _ := topo.monitors.Load(localPID.String())
		watchers := value.(*sync.Map)
		count := 0
		watchers.Range(func(_, _ interface{}) bool {
			count++
			return true
		})

		assert.Equal(t, 1, count, "should not add duplicate watchers")
	})
}

func TestTopology_HandleMonitorRelease(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, upstream, "local")

	localPID := relay.PID{Node: "local", Host: "host1", UniqID: "1"}.Precomputed()
	remotePID := relay.PID{Node: "remote", Host: "host2", UniqID: "2"}.Precomputed()

	err := topo.Register(localPID)
	require.NoError(t, err)

	err = topo.HandleMonitorRequest(remotePID, localPID)
	require.NoError(t, err)

	t.Run("HandleMonitorRelease removes caller from watchers", func(t *testing.T) {
		err := topo.HandleMonitorRelease(remotePID, localPID)
		require.NoError(t, err)

		_, ok := topo.monitors.Load(localPID.String())
		assert.False(t, ok, "should cleanup monitors when no watchers left")
	})

	t.Run("HandleMonitorRelease on non-monitored PID is safe", func(t *testing.T) {
		unmonitoredPID := relay.PID{Node: "local", Host: "host3", UniqID: "3"}.Precomputed()

		err := topo.HandleMonitorRelease(remotePID, unmonitoredPID)
		require.NoError(t, err)
	})
}

func TestTopology_HandleLinkRequest(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, upstream, "local")

	localPID := relay.PID{Node: "local", Host: "host1", UniqID: "1"}.Precomputed()
	remotePID := relay.PID{Node: "remote", Host: "host2", UniqID: "2"}.Precomputed()

	err := topo.Register(localPID)
	require.NoError(t, err)

	t.Run("HandleLinkRequest establishes remote side of link", func(t *testing.T) {
		err := topo.HandleLinkRequest(remotePID, localPID)
		require.NoError(t, err)

		links := topo.GetLinks(localPID)
		require.Len(t, links, 1, "should establish link")
		assert.Equal(t, remotePID, links[0])
	})

	t.Run("HandleLinkRequest on unregistered to PID fails", func(t *testing.T) {
		unregisteredPID := relay.PID{Node: "local", Host: "host3", UniqID: "3"}.Precomputed()

		err := topo.HandleLinkRequest(remotePID, unregisteredPID)
		require.Error(t, err)
		assert.True(t, errors.Is(err, topology.ErrPIDNotRegistered), "expected ErrPIDNotRegistered")
	})

	t.Run("HandleLinkRequest is idempotent", func(t *testing.T) {
		err := topo.HandleLinkRequest(remotePID, localPID)
		require.NoError(t, err)

		links := topo.GetLinks(localPID)
		assert.Len(t, links, 1, "should not create duplicate links")
	})
}

func TestTopology_HandleUnlinkRequest(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, upstream, "local")

	localPID := relay.PID{Node: "local", Host: "host1", UniqID: "1"}.Precomputed()
	remotePID := relay.PID{Node: "remote", Host: "host2", UniqID: "2"}.Precomputed()

	err := topo.Register(localPID)
	require.NoError(t, err)

	err = topo.HandleLinkRequest(remotePID, localPID)
	require.NoError(t, err)

	t.Run("HandleUnlinkRequest removes link", func(t *testing.T) {
		err := topo.HandleUnlinkRequest(remotePID, localPID)
		require.NoError(t, err)

		links := topo.GetLinks(localPID)
		assert.Len(t, links, 0, "should remove link")
	})

	t.Run("HandleUnlinkRequest on non-linked PID is safe", func(t *testing.T) {
		err := topo.HandleUnlinkRequest(remotePID, localPID)
		require.NoError(t, err)
	})
}

func TestTopology_RemoteMonitoringWithNotification(t *testing.T) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, upstream, "local")

	localPID := relay.PID{Node: "local", Host: "host1", UniqID: "1"}.Precomputed()
	remotePID := relay.PID{Node: "remote", Host: "host2", UniqID: "2"}.Precomputed()

	t.Run("Remote watcher receives exit notification", func(t *testing.T) {
		err := topo.Register(localPID)
		require.NoError(t, err)

		err = topo.HandleMonitorRequest(remotePID, localPID)
		require.NoError(t, err)

		upstream.reset()

		topo.Notify(localPID, &runtime.Result{
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
		assert.Equal(t, topology.KindExit, exitEvent.Kind)
		assert.Equal(t, localPID, exitEvent.From)
	})
}
