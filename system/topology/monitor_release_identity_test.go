// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"context"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
)

// Admission order is request/release/request. A returning older release must
// not remove the sender record of that last request.
func TestRemoteDemonitorPreservesNewerObservation(t *testing.T) {
	t.Run("concurrent successor", func(t *testing.T) {
		f := newMonitorSenderFixture(t)
		require.NoError(t, f.local.Monitor(f.caller, f.target))
		admitted, ack := make(chan struct{}), make(chan struct{})
		f.afterAdmission = func(p *relay.Package) error {
			if p.Messages[0].Payloads[0].Data().(map[string]any)["operation"] == topapi.MonitorRelease {
				close(admitted)
				<-ack
			}
			return nil
		}
		released, next := make(chan error, 1), make(chan error, 1)
		go func() { released <- f.local.Demonitor(f.caller, f.target) }()
		select {
		case <-admitted:
		case <-time.After(time.Second):
			t.Fatal("release not admitted")
		}
		go func() { next <- f.local.Monitor(f.caller, f.target) }()
		close(ack)
		require.NoError(t, <-released)
		require.NoError(t, <-next)
		require.Len(t, f.controls, 3)
		require.Equal(t, f.controls[1].reference, f.controls[2].previous)
		sh := f.local.getShard(f.caller.String())
		sh.mu.RLock()
		require.Contains(t, sh.processes[f.caller.String()].watching, f.target.String())
		sh.mu.RUnlock()
	})
	t.Run("absent relationship", func(t *testing.T) {
		f := newMonitorSenderFixture(t)
		require.NoError(t, f.local.Demonitor(f.caller, f.target))
		require.Empty(t, f.controls, "absent relationship cannot authorize a blind release")
	})
	t.Run("replacement caller", func(t *testing.T) {
		f := newMonitorSenderFixture(t)
		require.NoError(t, f.local.Monitor(f.caller, f.target))
		other := f.target
		other.UniqID = "other"
		require.NoError(t, f.remote.Register(other))
		f.afterAdmission = func(*relay.Package) error {
			f.afterAdmission = nil
			f.local.Remove(f.caller)
			require.NoError(t, f.local.Register(f.caller))
			require.NoError(t, f.local.Monitor(f.caller, other))
			return nil
		}
		require.ErrorIs(t, f.local.Demonitor(f.caller, f.target), topapi.ErrPIDNotRegistered)
		sh := f.local.getShard(f.caller.String())
		sh.mu.RLock()
		require.Contains(t, sh.processes[f.caller.String()].watching, other.String())
		sh.mu.RUnlock()
	})
}

func BenchmarkMonitorObservationLifecycle(b *testing.B) {
	for _, node := range []string{"local", "remote"} {
		b.Run(node, func(b *testing.B) {
			topo := NewTopology(nodeExitBoundaryRouter(func(pkg *relay.Package) error {
				relay.ReleasePackage(pkg)
				return nil
			}), "local")
			caller := pid.PID{Node: "local", Host: "app", UniqID: "caller"}
			target := pid.PID{Node: node, Host: "app", UniqID: "target"}
			caller, target = caller.Precomputed(), target.Precomputed()
			if err := topo.Register(caller); err != nil {
				b.Fatal(err)
			}
			if node == "local" {
				if err := topo.Register(target); err != nil {
					b.Fatal(err)
				}
			}
			if node == "remote" {
				peer := NewTopology(nodeExitBoundaryRouter(func(p *relay.Package) error { relay.ReleasePackage(p); return nil }), "remote")
				require.NoError(b, peer.Register(target))
				topo.router = monitorContextSender(func(ctx context.Context, p *relay.Package) error {
					p.ReceivedFrom = "local"
					_, reply, err := peer.PrepareRemoteMonitorReply(p, 1)
					if err != nil {
						return err
					}
					reply.ReceivedFrom = "remote"
					if err := topo.monitorEndpoint.SendContext(ctx, reply); err != nil {
						relay.ReleasePackage(reply)
						return err
					}
					relay.ReleasePackage(p)
					return nil
				})
				stop, err := topo.StartRemoteMonitoring(context.Background(), sysrelay.NewNode("local"), MonitorConfig{MaxPending: 1, RequestTimeout: time.Second})
				require.NoError(b, err)
				defer stop()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := topo.Monitor(caller, target); err != nil {
					b.Fatal(err)
				}
				if err := topo.Demonitor(caller, target); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
