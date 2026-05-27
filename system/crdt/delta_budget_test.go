// SPDX-License-Identifier: MPL-2.0

package crdt

import (
	"fmt"
	"testing"
)

// TestBroadcastQueue_DrainHonorsBudget verifies the regression behind Bug 7:
// when many entries are queued, Drain must NOT return more bytes than the
// caller's byteBudget; un-drained entries must remain in the queue for the
// next call.
func TestBroadcastQueue_DrainHonorsBudget(t *testing.T) {
	st := NewState("node-a")
	q := NewBroadcastQueue("node-a", 4096)

	// Stage ~50 entries — each encodes to ~80–120 bytes. Total well above
	// a single MTU.
	const N = 50
	for i := 0; i < N; i++ {
		key := fmt.Sprintf("key-%04d", i)
		val := []byte(fmt.Sprintf("value-payload-for-%04d-padding-padding-padding", i))
		e := st.Overwrite(key, val, int64(1000+i))
		q.Push(e)
	}
	if got := q.Depth(); got != N {
		t.Fatalf("queue depth before drain = %d want %d", got, N)
	}

	// Pretend memberlist gives us a 1398-byte UDP-cycle budget with a
	// 3-byte per-frame overhead (compoundOverhead+userMsgOverhead).
	const headerOverhead = 3
	const byteBudget = 1398

	frames := q.Drain(headerOverhead, byteBudget)
	if len(frames) == 0 {
		t.Fatalf("expected at least one frame, got 0")
	}

	totalCost := 0
	for _, f := range frames {
		totalCost += len(f) + headerOverhead
		if len(f) > MaxFrameBytes-headerOverhead {
			t.Errorf("frame too big: %d > %d", len(f), MaxFrameBytes-headerOverhead)
		}
	}
	if totalCost > byteBudget {
		t.Fatalf("Drain overshot budget: totalCost=%d budget=%d", totalCost, byteBudget)
	}

	// The remaining entries must still be queued — Bug 7 was that Drain
	// nuked all `pending` regardless of whether frames fit the budget.
	remaining := q.Depth()
	if remaining == 0 {
		t.Fatalf("Drain removed all entries despite tight budget; expected leftovers")
	}
	if remaining >= N {
		t.Fatalf("Drain removed nothing; depth %d == initial %d", remaining, N)
	}

	// Drain again with the same budget; we should make forward progress
	// across calls until empty.
	rounds := 1
	for q.Depth() > 0 && rounds < 100 {
		fs := q.Drain(headerOverhead, byteBudget)
		if len(fs) == 0 {
			t.Fatalf("Drain returned 0 frames with depth=%d (livelock)", q.Depth())
		}
		rounds++
	}
	if q.Depth() != 0 {
		t.Fatalf("queue not drained after %d rounds: depth=%d", rounds, q.Depth())
	}
}

// TestBroadcastQueue_DrainSmallBudget covers the Bug 8 regression: when
// byteBudget is well below MaxFrameBytes (e.g. the outer multiplex is
// splitting limit/N across delegates), Drain MUST still emit at least one
// short frame, not accumulate up to MaxFrameBytes worth of entries and then
// fail the budget check at flush time. Before this fix, Drain returned 0
// frames in this scenario and the queue grew without bound.
func TestBroadcastQueue_DrainSmallBudget(t *testing.T) {
	st := NewState("node-a")
	q := NewBroadcastQueue("node-a", 4096)

	const N = 50
	for i := 0; i < N; i++ {
		key := fmt.Sprintf("k-%04d", i)
		val := []byte(fmt.Sprintf("payload-%04d-fixed-fixed-fixed", i))
		q.Push(st.Overwrite(key, val, int64(i)))
	}

	// 699 ≈ what the outer fairness pass hands each delegate when limit is
	// 1398 and there are 2 delegates. Header overhead matches a delegate's
	// per-frame mux+name overhead (8+7).
	frames := q.Drain(15, 699)
	if len(frames) == 0 {
		t.Fatalf("Drain returned 0 frames at byteBudget=699 — small-budget livelock")
	}
	totalCost := 0
	for _, f := range frames {
		totalCost += len(f) + 15
	}
	if totalCost > 699 {
		t.Fatalf("Drain overshot small budget: totalCost=%d budget=699", totalCost)
	}
	if q.Depth() == N {
		t.Fatalf("Drain made no forward progress: depth still %d", q.Depth())
	}
}

// TestBroadcastQueue_DrainEmptyBudget ensures an absurdly tight budget
// (smaller than headerOverhead) returns nothing and keeps everything queued.
func TestBroadcastQueue_DrainEmptyBudget(t *testing.T) {
	st := NewState("node-a")
	q := NewBroadcastQueue("node-a", 4096)
	for i := 0; i < 10; i++ {
		e := st.Overwrite(fmt.Sprintf("k%d", i), []byte("v"), int64(i))
		q.Push(e)
	}
	frames := q.Drain(3, 2)
	if len(frames) != 0 {
		t.Fatalf("expected 0 frames at byteBudget=2, got %d", len(frames))
	}
	if q.Depth() != 10 {
		t.Fatalf("queue depth changed: %d want 10", q.Depth())
	}
}
