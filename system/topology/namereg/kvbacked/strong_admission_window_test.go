// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// A worker may pause immediately after the Raft ACK commits. The required
// node must have excluded competing local admissions before that point.
func TestStrongAckRequiresPriorLocalExclusion(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, time.Minute, nil)
	claimant := mkPID("node-1", "claimant")
	const attempt = "00000000000000000000000000000033"
	hdr, err := encode(pendingHeader{PID: claimant.String(), Name: "gap", AttemptID: attempt,
		RequiredNodes: []pid.NodeID{"node-1", "ghost"}, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(pendingKey("gap"), hdr); err != nil {
		t.Fatal(err)
	}
	entry, err := r.engine.Get(pendingKey("gap"))
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := r.strong.attest("gap", entry.Epoch, entry.Version, attempt, claimant, []pid.NodeID{"node-1", "ghost"}); err != nil || !ok {
		t.Fatalf("local ACK failed: %v", err)
	}
	if _, err := r.engine.Get(ackKey("gap", attempt, "node-1")); err != nil {
		t.Fatal("ACK not committed:", err)
	}
	if _, ok := r.IsStrongReserved("gap"); !ok {
		t.Fatal("ACK committed before this node excluded competing admissions")
	}
}

// KV publishes the complete promotion transaction before delivering its two
// watch events. Processing the pending DELETE must not open a hole before the
// active PUT is handled by the same observer.
func TestStrongPromotionDeleteKeepsExclusionUntilActiveObserved(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Minute, nil)
	claimant := mkPID("node-1", "claimant")
	const attempt = "00000000000000000000000000000034"
	old, err := encode(pendingHeader{PID: claimant.String(), Name: "promoted", AttemptID: attempt})
	if err != nil {
		t.Fatal(err)
	}
	r.strong.latch("promoted", attempt, claimant, 1)
	active, err := encode(activeValue{PID: claimant.String(), Name: "promoted", AttemptID: attempt, Strong: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(activeKey("promoted"), active); err != nil {
		t.Fatal(err)
	}
	r.handleWatchEvent(kvapi.WatchEvent{Type: kvapi.WatchDelete, Previous: &kvapi.Entry{Key: pendingKey("promoted"), Value: old}})
	if _, ok := r.IsStrongReserved("promoted"); !ok {
		t.Fatal("promotion released exclusion between pending DELETE and active PUT")
	}
}
