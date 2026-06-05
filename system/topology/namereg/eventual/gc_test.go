// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"testing"
	"time"
)

func TestGCRunner_DefaultWallFloorDisabled(t *testing.T) {
	state := NewState("node-A")
	pA := makePID("node-A", "h", "1")
	_, _ = reg(state, "alice", pA, 100)
	_ = state.Unregister("alice", 200)

	gc := NewGCRunner(GCConfig{
		State:   state,
		Tracker: NewTombstoneTracker(),
		AliveFn: func() map[string]struct{} {
			return map[string]struct{}{"node-A": {}}
		},
		Now: func() time.Time {
			return time.UnixMilli(1_000_000_000)
		},
	})
	gc.runOnce()

	if state.TombstoneCount() != 1 {
		t.Fatalf("default GC reaped an unacked tombstone; wall floor must be disabled by default")
	}
}
