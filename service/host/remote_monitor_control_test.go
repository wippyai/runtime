// SPDX-License-Identifier: MPL-2.0
package host

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	hostapi "github.com/wippyai/runtime/api/service/host"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/internode"
	systopology "github.com/wippyai/runtime/system/topology"
)

// Remote monitoring is runtime control, not a message requiring application
// code to install the monitor. This tests the real host/scheduler ingress and
// codec, without substituting the test-only topology monitor handler.
func TestRemoteMonitorControlDoesNotReachApplication(t *testing.T) {
	th := newTestHost()
	notices := make(chan *relay.Package, 1)
	topo := systopology.NewTopology(monitorNoticeRouter{notices: notices}, "test-node")
	replies := make(chan *relay.Package, 1)
	ctx := relay.WithRouter(topology.WithTopology(ctxWithAppContext(), topo), monitorReplySender(func(_ context.Context, p *relay.Package) error { replies <- p; return nil }))
	_, err := th.host.Start(ctx)
	require.NoError(t, err)
	defer th.stop()
	target := pid.PID{Node: "test-node", Host: "test:host", UniqID: "remote-monitor-target"}
	proc := &contextMessageProcess{ready: make(chan struct{}), received: make(chan struct{})}
	_, err = th.scheduler.Submit(context.Background(), target, proc, "", nil)
	require.NoError(t, err)
	select {
	case <-proc.ready:
	case <-time.After(time.Second):
		t.Fatal("target did not become ready")
	}
	require.NoError(t, topo.Register(target))
	caller := pid.PID{Node: "remote", Host: "sysreg"}
	pkg := relay.NewPackage(caller, target, topology.TopicEvents, payload.New(map[string]any{
		"v": uint64(1), "kind": topology.MonitorRequest, "ref": "installed", "previous": "", "caller": caller, "target": target,
	}))
	codec := internode.NewMessageCodec(nil)
	body, err := codec.Encode(pkg)
	relay.ReleasePackage(pkg)
	require.NoError(t, err)
	decoded, err := codec.Decode(body)
	require.NoError(t, err)
	decoded.ReceivedFrom = caller.Node
	err = th.host.Send(decoded)
	if err != nil {
		relay.ReleasePackage(decoded)
	}
	require.NoError(t, err)
	select {
	case reply := <-replies:
		require.Equal(t, "ok", reply.Messages[0].Payloads[0].Data().(map[string]any)["code"])
		relay.ReleasePackage(reply)
	case <-time.After(time.Second):
		t.Fatal("monitor acknowledgment not delivered")
	}
	topo.Complete(target, &runtime.Result{})
	select {
	case notice := <-notices:
		require.True(t, notice.Target.Equal(pid.PID{Node: caller.Node, Host: "topology"}))
		fields := notice.Messages[0].Payloads[0].Data().(map[string]any)
		require.Equal(t, "installed", fields["ref"])
		require.True(t, fields["caller"].(pid.PID).Equal(caller))
		relay.ReleasePackage(notice)
	case <-time.After(time.Second):
		t.Fatal("host failed to establish actual runtime monitor")
	}
	select {
	case <-proc.received:
		t.Fatal("control leaked to actor")
	default:
	}
	// A subsequent normal message still reaches the same actor.
	require.NoError(t, th.host.Send(relay.NewPackage(caller, target, "application", payload.New("data"))))
	select {
	case <-proc.received:
	case <-time.After(time.Second):
		t.Fatal("ordinary host delivery changed")
	}
}

type monitorNoticeRouter struct{ notices chan *relay.Package }

func (r monitorNoticeRouter) Send(pkg *relay.Package) error { r.notices <- pkg; return nil }

func TestRemoteMonitorLimitUpdateRequiresNewHost(t *testing.T) {
	current := &hostapi.EntryConfig{}
	desired := &hostapi.EntryConfig{}
	desired.HostConfig.RemoteMonitorLimit = hostapi.DefaultRemoteMonitorLimit
	require.Empty(t, unsupportedHostUpdateFields(current, desired, false))
	desired.HostConfig.RemoteMonitorLimit = 1
	require.Contains(t, unsupportedHostUpdateFields(current, desired, false), "host.remote_monitor_limit")
	desired.HostConfig.RemoteMonitorLimit = -1
	require.ErrorIs(t, desired.Validate(), hostapi.ErrInvalidRemoteMonitorLimit)
}

type monitorReplySender func(context.Context, *relay.Package) error

func (s monitorReplySender) Send(p *relay.Package) error { return s(context.Background(), p) }
func (s monitorReplySender) SendContext(ctx context.Context, p *relay.Package) error {
	return s(ctx, p)
}

func TestRemoteMonitorReplyBoundPrecedesMutationAndStopJoins(t *testing.T) {
	th := newTestHost()
	th.host.monitorReplies = make(chan struct{}, 1)
	entered := make(chan struct{})
	canceled := make(chan struct{})
	sender := monitorReplySender(func(ctx context.Context, p *relay.Package) error {
		close(entered)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	})
	topo := systopology.NewTopology(monitorNoticeRouter{notices: make(chan *relay.Package, 1)}, "test-node")
	ctx := relay.WithRouter(topology.WithTopology(ctxWithAppContext(), topo), sender)
	_, err := th.host.Start(ctx)
	require.NoError(t, err)
	defer th.stop()
	target := pid.PID{Node: "test-node", Host: "test:host", UniqID: "actor"}
	require.NoError(t, topo.Register(target))
	caller := pid.PID{Node: "remote", Host: "registry"}
	request := func(previous, ref string) *relay.Package {
		p := relay.NewPackage(caller, target, topology.TopicEvents, payload.New(map[string]any{"v": uint64(1), "kind": topology.MonitorRequest, "ref": ref, "previous": previous, "caller": caller, "target": target}))
		p.ReceivedFrom = caller.Node
		return p
	}
	require.NoError(t, th.host.Send(request("", "one")))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("reply not started")
	}
	successor := request("one", "two")
	require.Error(t, th.host.Send(successor))
	// Refusal preserved request ownership and did not install the successor.
	require.True(t, successor.Target.Equal(target))
	relay.ReleasePackage(successor)
	retry := request("", "one")
	_, err = topo.HandleRemoteMonitor(retry, hostapi.DefaultRemoteMonitorLimit)
	relay.ReleasePackage(retry)
	require.NoError(t, err, "capacity refusal mutated relationship")
	require.NoError(t, th.host.Stop(context.Background()))
	select {
	case <-canceled:
	default:
		t.Fatal("Stop returned before reply sender joined")
	}
	require.Empty(t, th.host.monitorReplies)
}

func TestRemoteMonitorReplyConfiguration(t *testing.T) {
	current, desired := &hostapi.EntryConfig{}, &hostapi.EntryConfig{}
	desired.HostConfig.RemoteMonitorReplies = hostapi.DefaultRemoteMonitorReplies
	desired.HostConfig.RemoteMonitorReplyTimeout = hostapi.DefaultRemoteMonitorReplyTimeout
	require.Empty(t, unsupportedHostUpdateFields(current, desired, false))
	desired.HostConfig.RemoteMonitorReplies = 1
	require.Contains(t, unsupportedHostUpdateFields(current, desired, false), "host.remote_monitor_replies")
	desired.HostConfig.RemoteMonitorReplyTimeout = time.Second
	require.Contains(t, unsupportedHostUpdateFields(current, desired, false), "host.remote_monitor_reply_timeout")
	require.NoError(t, desired.Validate())
	desired.HostConfig.RemoteMonitorReplies = -1
	require.Error(t, desired.Validate())
	desired.HostConfig.RemoteMonitorReplies = 1
	desired.HostConfig.RemoteMonitorReplyTimeout = -time.Second
	require.Error(t, desired.Validate())
}
