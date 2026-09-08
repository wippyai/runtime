// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	topapi "github.com/wippyai/runtime/api/topology"
)

// A reused PID string does not make a replacement process the issuer of a
// request that was still being admitted when the previous process completed.
func TestRemoteMonitorCannotAdoptReplacementCaller(t *testing.T) {
	for _, replace := range []bool{false, true} {
		name := "departed"
		if replace {
			name = "replacement"
		}
		t.Run(name, func(t *testing.T) {
			caller := pid.PID{Node: "local", Host: "app", UniqID: "caller"}
			target := pid.PID{Node: "remote", Host: "app", UniqID: "target"}
			var topo *Topology
			topo = NewTopology(nodeExitBoundaryRouter(func(pkg *relay.Package) error {
				defer relay.ReleasePackage(pkg)
				for _, msg := range pkg.Messages {
					for _, pl := range msg.Payloads {
						if _, ok := pl.Data().(*topapi.MonitorRequestEvent); ok {
							topo.Complete(caller, &runtime.Result{})
							if replace {
								require.NoError(t, topo.Register(caller))
							}
						}
					}
				}
				return nil
			}), "local")
			require.NoError(t, topo.Register(caller))
			err := topo.Monitor(caller, target)
			require.Error(t, err, "a departed caller cannot own successful monitor admission")
			sh := topo.getShard(caller.String())
			sh.mu.RLock()
			state, exists := sh.processes[caller.String()]
			watching := false
			if exists {
				_, watching = state.watching[target.String()]
			}
			sh.mu.RUnlock()
			require.Equal(t, replace, exists)
			require.False(t, watching, "late admission cannot attach to a replacement lifetime")
		})
	}
}
