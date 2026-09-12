// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
)

func TestPIDCleanupPreservesReplacementDuringExit(t *testing.T) {
	r := NewPIDRegistry()
	old := pid.PID{Node: "node", Host: "host", UniqID: "old"}
	replacement := pid.PID{Node: "node", Host: "host", UniqID: "replacement"}
	_, err := r.Register("app", old)
	require.NoError(t, err)
	value, _ := r.idToName.Load(old.String())
	names := value.(*pidNames)
	names.mu.Lock()
	done := make(chan struct{})
	go func() { r.Remove(old); close(done) }()
	// Hold cleanup after it takes the reverse index but before deleting names.
	var once sync.Once
	finish := func() { once.Do(func() { names.mu.Unlock(); <-done }) }
	defer finish()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, present := r.idToName.Load(old.String()); !present {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cleanup did not take reverse index")
		}
		runtime.Gosched()
	}
	require.True(t, r.Unregister("app"))
	_, err = r.Register("app", replacement)
	require.NoError(t, err)
	// Release/join before checking the winner, including on assertion failures.
	finish()
	got, found := r.LookupLocal("app")
	require.True(t, found, "old process cleanup erased replacement")
	require.True(t, got.Equal(replacement))
}

func TestPIDCleanupMatchesIdentityWithCachedRepresentation(t *testing.T) {
	r := NewPIDRegistry()
	owner := pid.PID{Node: "node", Host: "host", UniqID: "owner"}
	_, err := r.Register("app", owner.Precomputed())
	require.NoError(t, err)
	r.Remove(owner)
	_, found := r.LookupLocal("app")
	require.False(t, found)
}
