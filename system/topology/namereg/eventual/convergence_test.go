// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/pid"
)

// dotSpec describes one entry to apply, independent of any State's intern table.
type dotSpec struct {
	name     string
	origin   string
	pid      pid.PID
	counter  uint64
	wall     int64
	priority uint32
	deleted  bool
}

// applySpecs builds a fresh State and applies every spec in the given order,
// interning the origin string and rewriting Node to the local compact ID.
func applySpecs(local string, specs []dotSpec) *State {
	s := NewState(local)
	for _, sp := range specs {
		origin := s.internNode(sp.origin)
		e := &Entry{
			Name:     sp.name,
			PID:      sp.pid,
			Node:     origin,
			Counter:  sp.counter,
			Wall:     sp.wall,
			Priority: sp.priority,
			Deleted:  sp.deleted,
		}
		s.Apply(e)
	}
	return s
}

// permute yields every permutation of indices [0,n) via Heap's algorithm.
func permute(n int, yield func([]int)) {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	var rec func(k int)
	rec = func(k int) {
		if k == 1 {
			cp := make([]int, n)
			copy(cp, idx)
			yield(cp)
			return
		}
		for i := 0; i < k; i++ {
			rec(k - 1)
			if k%2 == 0 {
				idx[i], idx[k-1] = idx[k-1], idx[i]
			} else {
				idx[0], idx[k-1] = idx[k-1], idx[0]
			}
		}
	}
	rec(n)
}

// finalEntry returns the authoritative entry for a name as a comparable tuple,
// resolving the origin to its canonical string so independent intern tables
// compare equal.
type finalTuple struct {
	origin   string
	pidStr   string
	counter  uint64
	priority uint32
	deleted  bool
	present  bool
}

func snapshot(s *State, name string) finalTuple {
	sh := &s.shards[ShardFor(name)]
	sh.mu.RLock()
	rec, ok := sh.entries[name]
	var e *Entry
	if ok {
		e = s.winnerOf(rec)
	}
	sh.mu.RUnlock()
	if e == nil {
		return finalTuple{}
	}
	return finalTuple{
		present:  true,
		origin:   s.NodeString(e.Node),
		pidStr:   e.PID.String(),
		counter:  e.Counter,
		priority: e.Priority,
		deleted:  e.Deleted,
	}
}

// TestConvergence_AllPermutations is the core property test: a fixed multiset
// of dots — same-origin successions (A1<A2), multiple origins, a tombstone —
// must converge to the IDENTICAL authoritative entry no matter the apply order.
// The naive wall-LWW resolver fails this because cross-origin equal-wall ties
// keep whichever arrived first (split-brain) and Counter/PID-laden ordering is
// non-transitive (A1>B1, B1>A2, A2>A1 cycles).
func TestConvergence_AllPermutations(t *testing.T) {
	const name = "session-1"
	pA1 := makePID("node-A", "h", "a1")
	pA2 := makePID("node-A", "h", "a2")
	pB1 := makePID("node-B", "h", "b1")
	pC1 := makePID("node-C", "h", "c1")

	// Equal wall on every dot: forces the resolver onto its deterministic
	// tiebreak rather than physical clock. Includes the A1<A2 succession plus
	// B1, C1 from other origins.
	specs := []dotSpec{
		{name: name, origin: "node-A", pid: pA1, counter: 1, wall: 1000, priority: 0},
		{name: name, origin: "node-A", pid: pA2, counter: 2, wall: 1000, priority: 0},
		{name: name, origin: "node-B", pid: pB1, counter: 1, wall: 1000, priority: 0},
		{name: name, origin: "node-C", pid: pC1, counter: 1, wall: 1000, priority: 0},
	}

	var want finalTuple
	first := true
	count := 0
	permute(len(specs), func(order []int) {
		count++
		ordered := make([]dotSpec, len(specs))
		for i, j := range order {
			ordered[i] = specs[j]
		}
		s := applySpecs("node-local", ordered)
		got := snapshot(s, name)
		if first {
			want = got
			first = false
			return
		}
		if got != want {
			t.Fatalf("permutation %v diverged: got %+v want %+v", order, got, want)
		}
	})
	if count == 0 {
		t.Fatal("no permutations evaluated")
	}
	if !want.present || want.deleted {
		t.Fatalf("expected a live winner, got %+v", want)
	}
}

// TestConvergence_WithTombstone proves the observed-remove rule: a cross-origin
// tombstone must not suppress a different-origin live entry. The final result
// across all orders must be the deterministic live winner, never the tombstone.
func TestConvergence_WithTombstone(t *testing.T) {
	const name = "session-2"
	pA1 := makePID("node-A", "h", "a1")
	pB1 := makePID("node-B", "h", "b1")

	specs := []dotSpec{
		{name: name, origin: "node-A", pid: pA1, counter: 1, wall: 1000},
		{name: name, origin: "node-A", pid: pid.PID{}, counter: 2, wall: 1000, deleted: true}, // A relinquishes
		{name: name, origin: "node-B", pid: pB1, counter: 1, wall: 1000},                      // B's live claim
	}

	var want finalTuple
	first := true
	permute(len(specs), func(order []int) {
		ordered := make([]dotSpec, len(specs))
		for i, j := range order {
			ordered[i] = specs[j]
		}
		s := applySpecs("node-local", ordered)
		got := snapshot(s, name)
		if first {
			want = got
			first = false
			return
		}
		if got != want {
			t.Fatalf("permutation %v diverged: got %+v want %+v", order, got, want)
		}
	})
	// B's live claim must survive A's tombstone (observed-remove).
	if !want.present || want.deleted {
		t.Fatalf("live entry must beat cross-origin tombstone, got %+v", want)
	}
	if want.origin != "node-B" || want.pidStr != pB1.String() {
		t.Fatalf("expected node-B live winner, got %+v", want)
	}
}

// TestConvergence_NonTransitivityCycle is the targeted regression for the
// reviewer's counterexample: a key that mixes Counter/PID produces a cyclic
// order (A1>B1, B1>A2, A2>A1) and diverges by arrival order. With the fixed
// per-(name,origin) key, the cross-origin rank is invariant as Counter
// advances, so all orders converge.
func TestConvergence_NonTransitivityCycle(t *testing.T) {
	const name = "cycle"
	pA1 := makePID("node-A", "h", "a1")
	pA2 := makePID("node-A", "h", "a2")
	pB1 := makePID("node-B", "h", "b1")

	specs := []dotSpec{
		{name: name, origin: "node-A", pid: pA1, counter: 1, wall: 500},
		{name: name, origin: "node-B", pid: pB1, counter: 1, wall: 500},
		{name: name, origin: "node-A", pid: pA2, counter: 2, wall: 500},
	}

	results := map[finalTuple]int{}
	permute(len(specs), func(order []int) {
		ordered := make([]dotSpec, len(specs))
		for i, j := range order {
			ordered[i] = specs[j]
		}
		s := applySpecs("node-local", ordered)
		results[snapshot(s, name)]++
	})
	if len(results) != 1 {
		var b []string
		for k, v := range results {
			b = append(b, fmt.Sprintf("%+v x%d", k, v))
		}
		t.Fatalf("non-convergent: %d distinct outcomes: %v", len(results), b)
	}
}

// TestConvergence_HigherPriorityWins proves priority dominates the hash tie:
// a higher-priority cross-origin live entry wins regardless of arrival order.
func TestConvergence_HigherPriorityWins(t *testing.T) {
	const name = "prio"
	pLow := makePID("node-A", "h", "low")
	pHigh := makePID("node-B", "h", "high")

	specs := []dotSpec{
		{name: name, origin: "node-A", pid: pLow, counter: 1, wall: 9999, priority: 1},
		{name: name, origin: "node-B", pid: pHigh, counter: 1, wall: 1, priority: 5},
	}

	permute(len(specs), func(order []int) {
		ordered := make([]dotSpec, len(specs))
		for i, j := range order {
			ordered[i] = specs[j]
		}
		s := applySpecs("node-local", ordered)
		got := snapshot(s, name)
		if got.pidStr != pHigh.String() {
			t.Fatalf("higher priority must win in order %v, got %+v", order, got)
		}
	})
}

// TestState_SameOriginDeleteWins keeps the same-dot delete-wins guarantee:
// a tombstone with equal counter from the SAME origin beats the live entry.
func TestState_SameOriginDeleteWins(t *testing.T) {
	s := NewState("node-A")
	originB := s.internNode("node-B")

	live := &Entry{Name: "x", PID: makePID("node-B", "h", "1"), Node: originB, Counter: 7, Wall: 100}
	tomb := &Entry{Name: "x", Node: originB, Counter: 7, Wall: 100, Deleted: true}

	s.Apply(live)
	if outcome, _, _ := s.Apply(tomb); outcome != MergeDeleteWins {
		t.Fatalf("expected MergeDeleteWins, got %d", outcome)
	}
	if _, found := s.Lookup("x"); found {
		t.Fatalf("same-origin tombstone must beat live entry on equal dot")
	}
}
