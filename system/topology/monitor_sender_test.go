// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	topapi "github.com/wippyai/runtime/api/topology"
	sysrelay "github.com/wippyai/runtime/system/relay"
)

type monitorSenderFixture struct {
	deliveryTargets []pid.PID
	deliveries      []map[string]any
	deliveryErr     error
	local, remote   *Topology
	caller, target  pid.PID
	controls        []remoteMonitorControl
	afterAdmission  func(*relay.Package) error
}

func newMonitorSenderFixture(t *testing.T) *monitorSenderFixture {
	t.Helper()
	f := &monitorSenderFixture{caller: pid.PID{Node: "client", Host: "registry", UniqID: "caller"}, target: pid.PID{Node: "server", Host: "process", UniqID: "actor"}}
	f.remote = NewTopology(monitorContextSender(func(ctx context.Context, p *relay.Package) error {
		decoded := monitorCodecCopy(t, p, "server")
		err := f.local.monitorEndpoint.SendContext(ctx, decoded)
		if err != nil {
			relay.ReleasePackage(decoded)
			return err
		}
		relay.ReleasePackage(p)
		return nil
	}), "server")
	require.NoError(t, f.remote.Register(f.target))
	sender := monitorContextSender(func(ctx context.Context, p *relay.Package) error {
		if p.Target.Node == "client" {
			if f.deliveryErr != nil {
				return f.deliveryErr
			}
			switch body := p.Messages[0].Payloads[0].Data().(type) {
			case map[string]any:
				f.deliveries = append(f.deliveries, body)
			case *topapi.ExitEvent:
				f.deliveries = append(f.deliveries, map[string]any{"kind": body.Kind, "from": body.From, "result": body.Result})
			default:
				t.Fatalf("unexpected consumer event %T", body)
			}
			f.deliveryTargets = append(f.deliveryTargets, p.Target)
			relay.ReleasePackage(p)
			return nil
		}
		request := monitorCodecCopy(t, p, "client")
		defer relay.ReleasePackage(request)
		control, err := decodeRemoteMonitor(request, "server")
		require.NoError(t, err)
		require.NotNil(t, control)
		f.controls = append(f.controls, *control)
		handled, reply, err := f.remote.PrepareRemoteMonitorReply(request, 4)
		require.True(t, handled)
		require.NoError(t, err)
		defer relay.ReleasePackage(reply)
		if f.afterAdmission != nil {
			if err := f.afterAdmission(reply); err != nil {
				return err
			}
		}
		require.NoError(t, f.local.monitorEndpoint.SendContext(ctx, monitorCodecCopy(t, reply, "server")))
		relay.ReleasePackage(p)
		return nil
	})
	f.local = NewTopology(sender, "client")
	require.NoError(t, f.local.Register(f.caller))
	stop, err := f.local.StartRemoteMonitoring(context.Background(), sysrelay.NewNode("client"), MonitorConfig{MaxPending: 4, MaxRecordsPerCaller: 2, RequestTimeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stop()) })
	return f
}

func TestMonitorSenderAcknowledgedLifecycleAndPredecessor(t *testing.T) {
	f := newMonitorSenderFixture(t)
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	require.Len(t, f.controls, 1, "already established monitor should not resend")
	first := f.controls[0].reference
	require.NoError(t, f.local.Demonitor(f.caller, f.target))
	require.NoError(t, f.local.Demonitor(f.caller, f.target))
	require.Len(t, f.controls, 2)
	require.Equal(t, first, f.controls[1].reference)
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	require.Len(t, f.controls, 3)
	require.Equal(t, first, f.controls[2].previous)
	require.NotEqual(t, first, f.controls[2].reference)
}

func TestMonitorSenderReconcilesLostInstallationBeforeRelease(t *testing.T) {
	f := newMonitorSenderFixture(t)
	f.afterAdmission = func(*relay.Package) error { return context.DeadlineExceeded }
	require.ErrorIs(t, f.local.Monitor(f.caller, f.target), context.DeadlineExceeded)
	sh := f.local.getShard(f.caller.String())
	sh.mu.RLock()
	require.Empty(t, sh.processes[f.caller.String()].watching, "send admission is not remote establishment")
	sh.mu.RUnlock()
	f.afterAdmission = nil
	require.NoError(t, f.local.Demonitor(f.caller, f.target))
	require.Len(t, f.controls, 3)
	require.Equal(t, topapi.MonitorRequest, f.controls[1].kind)
	require.Equal(t, topapi.MonitorRelease, f.controls[2].kind)
	require.Equal(t, f.controls[0].reference, f.controls[1].reference)
	require.Equal(t, f.controls[0].reference, f.controls[2].reference)
}

func TestMonitorSenderLateAcknowledgmentCannotAdoptReplacement(t *testing.T) {
	f := newMonitorSenderFixture(t)
	f.afterAdmission = func(*relay.Package) error {
		f.local.Remove(f.caller)
		require.NoError(t, f.local.Register(f.caller))
		return nil
	}
	require.ErrorIs(t, f.local.Monitor(f.caller, f.target), topapi.ErrPIDNotRegistered)
	sh := f.local.getShard(f.caller.String())
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	require.Empty(t, sh.processes[f.caller.String()].watching)
	require.Empty(t, sh.processes[f.caller.String()].remoteWatching)
}

func TestMonitorSenderCompletionReleasesUncertainReference(t *testing.T) {
	f := newMonitorSenderFixture(t)
	f.afterAdmission = func(*relay.Package) error { return context.DeadlineExceeded }
	require.ErrorIs(t, f.local.Monitor(f.caller, f.target), context.DeadlineExceeded)
	f.afterAdmission = nil
	f.local.Complete(f.caller, &runtime.Result{})
	require.Len(t, f.controls, 2)
	require.Equal(t, topapi.MonitorRelease, f.controls[1].kind)
	require.Equal(t, f.controls[0].reference, f.controls[1].reference)
	retry := monitorAdmissionPackage(t, f.caller, f.target, topapi.MonitorRequest, "", f.controls[0].reference)
	_, err := f.remote.HandleRemoteMonitor(retry, 4)
	relay.ReleasePackage(retry)
	require.ErrorIs(t, err, errRemoteMonitorConflict, "completed caller cannot be reinstalled by delayed initial request")
}

func TestMonitorSenderReconcilesLostReleaseBeforeSuccessor(t *testing.T) {
	f := newMonitorSenderFixture(t)
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	f.afterAdmission = func(*relay.Package) error { return context.DeadlineExceeded }
	require.ErrorIs(t, f.local.Demonitor(f.caller, f.target), context.DeadlineExceeded)
	f.afterAdmission = nil
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	require.Len(t, f.controls, 4)
	require.Equal(t, topapi.MonitorRelease, f.controls[2].kind)
	require.Equal(t, f.controls[1].reference, f.controls[2].reference)
	require.Equal(t, topapi.MonitorRequest, f.controls[3].kind)
	require.Equal(t, f.controls[2].reference, f.controls[3].previous)
	require.NotEqual(t, f.controls[2].reference, f.controls[3].reference)
}

func TestMonitorSenderRetiredRecordsRemainBounded(t *testing.T) {
	f := newMonitorSenderFixture(t)
	for _, name := range []string{"actor", "second"} {
		target := f.target
		target.UniqID = name
		if name != "actor" {
			require.NoError(t, f.remote.Register(target))
		}
		require.NoError(t, f.local.Monitor(f.caller, target))
		require.NoError(t, f.local.Demonitor(f.caller, target))
	}
	target := f.target
	target.UniqID = "third"
	require.NoError(t, f.remote.Register(target))
	require.ErrorIs(t, f.local.Monitor(f.caller, target), errRemoteMonitorCapacity)
	require.Len(t, f.controls, 4, "record limit must refuse before network admission")
	// Reusing an existing relationship remains possible at capacity.
	require.NoError(t, f.local.Monitor(f.caller, f.target))
}

func TestMonitorSenderDisconnectRequiresRevalidationWithoutInventingDeath(t *testing.T) {
	f := newMonitorSenderFixture(t)
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	ref := f.controls[0].reference
	f.local.HandleNodeExit(f.target.Node, errors.New("connection lost"))
	require.Len(t, f.deliveries, 1)
	require.Equal(t, topapi.LinkDown, f.deliveries[0]["kind"])
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	require.Len(t, f.controls, 2, "explicit retry must revalidate after disconnect")
	require.Equal(t, ref, f.controls[1].reference, "connectivity loss does not create a new owner")
	f.remote.Complete(f.target, &runtime.Result{})
	require.Len(t, f.deliveries, 2)
	require.Equal(t, topapi.Exit, f.deliveries[1]["kind"])
}

func TestMonitorSenderAcknowledgmentDoesNotClearConcurrentDisconnect(t *testing.T) {
	f := newMonitorSenderFixture(t)
	f.afterAdmission = func(*relay.Package) error {
		f.local.HandleNodeExit(f.target.Node, errors.New("disconnect before acknowledgment"))
		return nil
	}
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	f.afterAdmission = nil
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	require.Len(t, f.controls, 2, "acknowledgment from prior connectivity generation must not suppress revalidation")
	require.Equal(t, f.controls[0].reference, f.controls[1].reference)
}
