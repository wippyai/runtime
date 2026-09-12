// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/internode"
)

func monitorAdmissionPackage(t *testing.T, caller, target pid.PID, kind, previous, reference string) *relay.Package {
	t.Helper()
	fields := map[string]any{"v": remoteMonitorVersion, "kind": kind, "ref": reference, "caller": caller, "target": target}
	if kind == topapi.MonitorRequest {
		fields["previous"] = previous
	}
	pkg := relay.NewPackage(caller, target, topapi.TopicEvents, payload.New(fields))
	codec := internode.NewMessageCodec(nil)
	body, err := codec.Encode(pkg)
	relay.ReleasePackage(pkg)
	require.NoError(t, err)
	decoded, err := codec.Decode(body)
	require.NoError(t, err)
	decoded.ReceivedFrom = caller.Node
	return decoded
}

func TestRemoteMonitorTargetLifetimeAndCompletionReference(t *testing.T) {
	var notices []remoteMonitorObserver
	topo := NewTopology(nodeExitBoundaryRouter(func(pkg *relay.Package) error {
		defer relay.ReleasePackage(pkg)
		notice := pkg.Messages[0].Payloads[0].Data().(map[string]any)
		require.Equal(t, topapi.Exit, notice["kind"])
		require.True(t, pkg.Target.Equal(pid.PID{Node: "remote", Host: monitorControlHostID}))
		notices = append(notices, remoteMonitorObserver{caller: notice["caller"].(pid.PID), reference: notice["ref"].(string)})
		return nil
	}), "local")
	caller := pid.PID{Node: "remote", Host: "sysreg"}
	target := pid.PID{Node: "local", Host: "process", UniqID: "target"}
	apply := func(kind, previous, reference string) error {
		pkg := monitorAdmissionPackage(t, caller, target, kind, previous, reference)
		defer relay.ReleasePackage(pkg)
		handled, err := topo.HandleRemoteMonitor(pkg, 1)
		require.True(t, handled)
		return err
	}
	require.ErrorIs(t, apply(topapi.MonitorRequest, "", "one"), topapi.ErrPIDNotRegistered)
	require.NoError(t, topo.Register(target))
	require.NoError(t, apply(topapi.MonitorRequest, "", "one"))
	require.NoError(t, apply(topapi.MonitorRelease, "", "one"))
	require.ErrorIs(t, apply(topapi.MonitorRequest, "", "one"), errRemoteMonitorConflict)
	require.NoError(t, apply(topapi.MonitorRequest, "one", "two"))
	require.ErrorIs(t, apply(topapi.MonitorRelease, "", "one"), errRemoteMonitorConflict)
	topo.Complete(target, &runtime.Result{})
	require.Len(t, notices, 1)
	require.True(t, notices[0].caller.Equal(caller))
	require.Equal(t, "two", notices[0].reference)
	topo.Complete(target, &runtime.Result{})
	require.Len(t, notices, 1)
	require.ErrorIs(t, apply(topapi.MonitorRequest, "two", "three"), topapi.ErrPIDNotRegistered)
}

func TestRemoteMonitorRemovalSealsRecordSet(t *testing.T) {
	topo := NewTopology(nodeExitBoundaryRouter(func(pkg *relay.Package) error { relay.ReleasePackage(pkg); return nil }), "local")
	caller := pid.PID{Node: "remote", Host: "sysreg"}
	target := pid.PID{Node: "local", Host: "process", UniqID: "target"}
	require.NoError(t, topo.Register(target))
	pkg := monitorAdmissionPackage(t, caller, target, topapi.MonitorRequest, "", "one")
	handled, err := topo.HandleRemoteMonitor(pkg, 1)
	relay.ReleasePackage(pkg)
	require.True(t, handled)
	require.NoError(t, err)
	sh := topo.getShard(target.String())
	sh.mu.RLock()
	owned := sh.processes[target.String()].remoteObservers
	sh.mu.RUnlock()
	topo.Remove(target)
	require.ErrorIs(t, owned.establish(caller, "one", "two"), errRemoteMonitorClosed)
}
