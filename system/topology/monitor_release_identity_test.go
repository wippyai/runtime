// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
)

// Admission order is request/release/request. A returning older release must
// not remove the sender record of that last request.
func TestRemoteDemonitorPreservesNewerObservation(t *testing.T) {
	caller := pid.PID{Node: "local", Host: "app", UniqID: "caller"}
	target := pid.PID{Node: "remote", Host: "app", UniqID: "target"}
	for _, mode := range []string{"existing", "absent", "replaced-caller"} {
		t.Run(mode, func(t *testing.T) {
			var topo *Topology
			var admitted []topapi.Kind
			topo = NewTopology(nodeExitBoundaryRouter(func(pkg *relay.Package) error {
				defer relay.ReleasePackage(pkg)
				for _, msg := range pkg.Messages {
					for _, pl := range msg.Payloads {
						switch pl.Data().(type) {
						case *topapi.MonitorRequestEvent:
							admitted = append(admitted, topapi.MonitorRequest)
						case *topapi.MonitorReleaseEvent:
							admitted = append(admitted, topapi.MonitorRelease)
							// The release is admitted before the newer monitor.
							if mode == "replaced-caller" {
								topo.Remove(caller)
								require.NoError(t, topo.Register(caller))
							}
							require.NoError(t, topo.Monitor(caller, target))
						}
					}
				}
				return nil
			}), "local")
			require.NoError(t, topo.Register(caller))
			if mode != "absent" {
				require.NoError(t, topo.Monitor(caller, target))
			}
			require.NoError(t, topo.Demonitor(caller, target))
			want := []topapi.Kind{topapi.MonitorRelease, topapi.MonitorRequest}
			if mode != "absent" {
				want = append([]topapi.Kind{topapi.MonitorRequest}, want...)
			}
			require.Equal(t, want, admitted)
			sh := topo.getShard(caller.String())
			sh.mu.RLock()
			_, watching := sh.processes[caller.String()].watching[target.String()]
			sh.mu.RUnlock()
			require.True(t, watching, "older release must retain the newer sender observation")
		})
	}
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
