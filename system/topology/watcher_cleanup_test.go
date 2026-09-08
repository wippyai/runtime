// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
)

func TestDepartedObserverReleasesLocalWatcher(t *testing.T) {
	for _, complete := range []bool{false, true} {
		name := "remove"
		if complete {
			name = "complete"
		}
		t.Run(name, func(t *testing.T) {
			router := newMockUpstream()
			topo := NewTopology(router, "local")
			caller := pid.PID{Node: "local", Host: "process", UniqID: "observer"}
			target := pid.PID{Node: "local", Host: "process", UniqID: "service"}
			other := pid.PID{Node: "local", Host: "process", UniqID: "other-observer"}
			for _, p := range []pid.PID{caller, target, other} {
				require.NoError(t, topo.Register(p))
			}
			require.NoError(t, topo.Monitor(caller, target))
			require.NoError(t, topo.Monitor(other, target))
			if complete {
				topo.Complete(caller, &runtime.Result{})
			} else {
				topo.Remove(caller)
			}
			key := target.String()
			sh := topo.getShard(key)
			sh.mu.RLock()
			_, stale := sh.processes[key].watchers[caller.String()]
			_, retained := sh.processes[key].watchers[other.String()]
			sh.mu.RUnlock()
			require.False(t, stale, "a live service must not retain a departed observer")
			require.True(t, retained, "unrelated observers remain installed")
			topo.Complete(target, &runtime.Result{})
			require.Empty(t, router.getSends(caller), "do not send a later service exit to a dead observer")
			require.Len(t, router.getSends(other), 1)
		})
	}
}
