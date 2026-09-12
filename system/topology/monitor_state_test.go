// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
)

func TestRemoteMonitorReferenceTransitions(t *testing.T) {
	set, err := newRemoteMonitorSet(1)
	require.NoError(t, err)
	caller := pid.PID{Node: "remote", Host: "sysreg"}
	cached := caller.Precomputed()
	require.NoError(t, set.establish(caller, "", "one"))
	require.NoError(t, set.establish(cached, "", "one"))
	require.NoError(t, set.release(caller, "one"))
	require.NoError(t, set.release(cached, "one"))
	require.ErrorIs(t, set.establish(caller, "", "one"), errRemoteMonitorConflict, "released request resurrected")
	require.NoError(t, set.establish(caller, "one", "two"))
	require.ErrorIs(t, set.release(caller, "one"), errRemoteMonitorConflict, "stale release removed successor")
	require.ErrorIs(t, set.establish(caller, "", "old"), errRemoteMonitorConflict)
	require.Equal(t, []remoteMonitorObserver{{caller: caller, reference: "two"}}, set.close())
	require.Empty(t, set.close())
	require.ErrorIs(t, set.establish(caller, "two", "three"), errRemoteMonitorClosed)
}

func TestRemoteMonitorBoundIncludesRetiredObservers(t *testing.T) {
	set, err := newRemoteMonitorSet(1)
	require.NoError(t, err)
	caller := pid.PID{Node: "remote", Host: "app", UniqID: "caller"}
	previous := ""
	for i := 0; i < 1000; i++ {
		next := fmt.Sprint(i)
		require.NoError(t, set.establish(caller, previous, next))
		require.NoError(t, set.release(caller, next))
		previous = next
	}
	require.Len(t, set.records, 1)
	require.ErrorIs(t, set.establish(pid.PID{Node: "other", Host: "app"}, "", "new"), errRemoteMonitorCapacity)
	require.Empty(t, set.close(), "retired observer notified as live")
}

func TestRemoteMonitorConcurrentSuccessorsHaveOneWinner(t *testing.T) {
	set, err := newRemoteMonitorSet(1)
	require.NoError(t, err)
	caller := pid.PID{Node: "remote", Host: "app"}
	require.NoError(t, set.establish(caller, "", "first"))
	var workers sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		workers.Go(func() { results <- set.establish(caller, "first", fmt.Sprintf("next-%d", i)) })
	}
	workers.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, errRemoteMonitorConflict)
		}
	}
	require.Equal(t, 1, winners)
	require.Len(t, set.close(), 1)
}
