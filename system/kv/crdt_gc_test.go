// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"errors"
	"testing"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// TestCRDTEngine_TombstoneGCBoundsMemory is the B2 regression: every Delete
// leaves a tombstone. The default is correctness-first and keeps tombstones
// because a wall-clock floor can resurrect deletes after long partitions; an
// explicitly configured positive floor bounds delete-churn memory.
func TestCRDTEngine_TombstoneGCBoundsMemory(t *testing.T) {
	// A tombstone is retained under the default disabled wall floor.
	retain := newCRDT(t, "n1")
	if _, err := retain.Set("k", []byte("v")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := retain.Delete("k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if safe, floor := retain.gcTombstones(); safe+floor != 0 || retain.state.TombstoneCount() != 1 {
		t.Fatalf("tombstone reaped under disabled default floor: safe=%d floor=%d count=%d",
			safe, floor, retain.state.TombstoneCount())
	}

	// Wrong/non-positive floors are treated as disabled, not as "reap
	// everything"; this avoids data loss from bad config values.
	disabled := newCRDT(t, "n2")
	disabled.SetTombstoneRetention(-time.Second)
	if _, err := disabled.Set("k", []byte("v")); err != nil {
		t.Fatalf("set disabled: %v", err)
	}
	if err := disabled.Delete("k"); err != nil {
		t.Fatalf("delete disabled: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if safe, floor := disabled.gcTombstones(); safe+floor != 0 || disabled.state.TombstoneCount() != 1 {
		t.Fatalf("negative floor should not reap: safe=%d floor=%d count=%d",
			safe, floor, disabled.state.TombstoneCount())
	}

	// Past an explicit positive floor every tombstone is reaped, so put/delete
	// churn can be bounded when the operator accepts the max-partition tradeoff.
	gc := newCRDT(t, "n3")
	gc.SetTombstoneRetention(time.Millisecond)
	for _, k := range []string{"a", "b", "c"} {
		if _, err := gc.Set(k, []byte("v")); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
		if err := gc.Delete(k); err != nil {
			t.Fatalf("delete %s: %v", k, err)
		}
	}
	if got := gc.state.TombstoneCount(); got != 3 {
		t.Fatalf("want 3 tombstones before GC, got %d", got)
	}
	time.Sleep(10 * time.Millisecond)
	if safe, floor := gc.gcTombstones(); safe+floor != 3 {
		t.Fatalf("want 3 reaped past floor, got safe=%d floor=%d", safe, floor)
	}
	if got := gc.state.TombstoneCount(); got != 0 {
		t.Fatalf("tombstones not reaped: %d remain", got)
	}
	if _, err := gc.Get("a"); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("reaped key a: want ErrKeyNotFound, got %v", err)
	}
}
