// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"errors"
	"io"
	"testing"
	"time"

	hraft "github.com/hashicorp/raft"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// TestRaftEngine_LeaseDeadlineHonoredAcrossReArm is the B4 regression: a leader
// re-arming a lease must honor its replicated absolute deadline, not reset the
// clock to now+TTL. A lease granted on a prior leader with a large TTL but an
// already-past deadline must expire promptly; the pre-fix re-arm rebuilt the
// deadline as now+TTL and kept it alive (up to N×TTL under N failovers).
func TestRaftEngine_LeaseDeadlineHonoredAcrossReArm(t *testing.T) {
	eng, fsm := newEngine(t)

	past := time.Now().Add(-2 * time.Second).UnixMilli()
	fsm.Apply(&hraft.Log{Index: 1, Data: encodeCommand(command{Op: opLeaseGrant, LeaseID: "L", TTLms: 60_000, ExpiresAtMs: past})})
	fsm.Apply(&hraft.Log{Index: 2, Data: encodeCommand(command{Op: opSetWithLease, Key: "lk", Value: []byte("v"), LeaseID: "L"})})

	if _, err := eng.Get("lk"); err != nil {
		t.Fatalf("leased key should exist before expiry: %v", err)
	}

	// The leader sweeper re-arms from the replicated absolute deadline (already in
	// the past) and revokes the lease, deleting its key within a sweep interval.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := eng.Get("lk"); errors.Is(err, kvapi.ErrKeyNotFound) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("lease with a past absolute deadline was not expired (re-arm reset the clock)")
}

// TestRaftFSM_LeaseDeadlineSurvivesSnapshot proves the absolute deadline is
// persisted, so a node restoring from a snapshot re-arms to the original expiry.
func TestRaftFSM_LeaseDeadlineSurvivesSnapshot(t *testing.T) {
	fsm := NewRaftFSM(nil)
	exp := time.Now().Add(42 * time.Second).UnixMilli()
	fsm.Apply(&hraft.Log{Index: 1, Data: encodeCommand(command{Op: opLeaseGrant, LeaseID: "L", TTLms: 42_000, ExpiresAtMs: exp})})

	snap, err := fsm.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	var sink memSink
	if err := snap.Persist(&sink); err != nil {
		t.Fatalf("persist: %v", err)
	}

	restored := NewRaftFSM(nil)
	if err := restored.Restore(io.NopCloser(&sink)); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := restored.leaseSnapshot()["L"]; got != exp {
		t.Fatalf("restored lease deadline = %d, want %d (absolute deadline not persisted)", got, exp)
	}
}
