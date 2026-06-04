// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"errors"
	"testing"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// TestCRDTEngine_TombstoneGCBoundsMemory is the B2 regression: every Delete
// leaves a tombstone, and without GC they accumulate forever in state, in every
// gossip frame, and in every durable snapshot. The wall-floor reaper bounds that
// while retaining recent tombstones so a lagging peer cannot resurrect a delete.
func TestCRDTEngine_TombstoneGCBoundsMemory(t *testing.T) {
	// A recent tombstone is retained under the default wall floor.
	retain := newCRDT(t, "n1")
	if _, err := retain.Set("k", []byte("v")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := retain.Delete("k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if safe, floor := retain.gcTombstones(); safe+floor != 0 || retain.state.TombstoneCount() != 1 {
		t.Fatalf("recent tombstone reaped under default floor: safe=%d floor=%d count=%d",
			safe, floor, retain.state.TombstoneCount())
	}

	// Past the floor every tombstone is reaped, so put/delete churn cannot grow
	// state without bound.
	gc := newCRDT(t, "n2")
	gc.tombstoneFloor = 0
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
