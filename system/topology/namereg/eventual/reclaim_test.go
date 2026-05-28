// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
)

// scanRefs walks every shard and counts, per compact origin id, the number of
// records that currently hold a dot from that origin (live OR tombstone). This
// is the ground truth that nodeRefs must match exactly.
func scanRefs(s *State) map[uint32]uint64 {
	got := make(map[uint32]uint64)
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.RLock()
		for _, rec := range sh.entries {
			for id := range rec.dots {
				got[id]++
			}
		}
		sh.mu.RUnlock()
	}
	return got
}

// assertRefsConsistent checks nodeRefs[id] == actual dot count for every id.
func assertRefsConsistent(t *testing.T, s *State) {
	t.Helper()
	want := scanRefs(s)
	s.cvMu.RLock()
	refs := make([]uint64, len(s.nodeRefs))
	copy(refs, s.nodeRefs)
	s.cvMu.RUnlock()

	for id := uint32(0); int(id) < len(refs); id++ {
		if refs[id] != want[id] {
			t.Fatalf("refcount mismatch for id %d: nodeRefs=%d actual=%d", id, refs[id], want[id])
		}
	}
	// Any id present in the scan beyond the refs slice is also a violation.
	for id, c := range want {
		if int(id) >= len(refs) {
			t.Fatalf("refcount missing for id %d: nodeRefs absent, actual=%d", id, c)
		}
	}
}

// TestReclaim_RefcountInvariant_Randomized is the safety net. After each step in
// a randomized sequence of Register/Apply(remote)/Unregister/ReapNode/
// ReapTombstones, nodeRefs[id] must exactly equal the live count of records
// holding a dot from id. A missed retain/release surfaces here.
func TestReclaim_RefcountInvariant_Randomized(t *testing.T) {
	for iter := 0; iter < 200; iter++ {
		seed := int64(iter)*7919 + 13
		rng := rand.New(rand.NewSource(seed))
		s := NewState("node-local")

		// A pool of foreign origins and names.
		origins := []string{"node-A", "node-B", "node-C", "node-D"}
		originID := map[string]uint32{}
		for _, o := range origins {
			originID[o] = s.internNode(o)
		}
		counters := map[uint32]uint64{}
		names := make([]string, 12)
		for i := range names {
			names[i] = fmt.Sprintf("name-%d", i)
		}

		const steps = 60
		for step := 0; step < steps; step++ {
			name := names[rng.Intn(len(names))]
			switch rng.Intn(6) {
			case 0: // local register
				p := makePID("node-local", "h", fmt.Sprintf("p%d", step))
				s.Register(name, p, int64(step), uint32(rng.Intn(3)))
			case 1: // local unregister
				s.Unregister(name, int64(step))
			case 2, 3: // remote apply (new or higher counter)
				o := origins[rng.Intn(len(origins))]
				id := originID[o]
				counters[id]++
				e := &Entry{
					Name:     name,
					PID:      makePID(o, "h", fmt.Sprintf("r%d", step)),
					Node:     id,
					Counter:  counters[id],
					Wall:     int64(step),
					Priority: uint32(rng.Intn(3)),
				}
				s.Apply(e)
			case 4: // reap a departed node's dots in place
				o := origins[rng.Intn(len(origins))]
				s.ReapNode(o)
			case 5: // reap tombstones aggressively (high safe counters + wall floor 0)
				safe := s.CVSnapshot()
				for i := range safe {
					safe[i] = 1 << 62
				}
				s.ReapTombstones(safe, int64(step)+1_000_000, 0)
			}
			assertRefsConsistent(t, s)
		}
	}
}

// TestReclaim_HappensAndSlotReused proves a departed (not-alive) node whose dots
// are all reaped gets its id reclaimed, and the freed slot is reused by the next
// new origin without growing stringIDs.
func TestReclaim_HappensAndSlotReused(t *testing.T) {
	s := NewState("node-local")
	idN := s.internNode("node-N")

	// node-N owns a name; apply then tombstone-and-reap it fully.
	e := &Entry{Name: "x", PID: makePID("node-N", "h", "1"), Node: idN, Counter: 1, Wall: 10}
	s.Apply(e)
	tombs := s.ReapNode("node-N")
	if len(tombs) != 1 {
		t.Fatalf("expected one tombstone, got %d", len(tombs))
	}
	// Drop the tombstone so node-N has zero dots.
	safe := s.CVSnapshot()
	for i := range safe {
		safe[i] = 1 << 62
	}
	s.ReapTombstones(safe, 1_000_000, 0)

	lenBefore := len(s.StringIDs())

	// node-N is not alive → reclaimed.
	alive := map[string]struct{}{"node-local": {}}
	if n := s.ReclaimUnreferencedNodes(alive); n != 1 {
		t.Fatalf("expected 1 reclaim, got %d", n)
	}
	if got := s.NodeString(idN); got != "" {
		t.Errorf("reclaimed slot should be empty, got %q", got)
	}

	// The next new origin reuses the freed slot — no growth.
	idM := s.internNode("node-M")
	if idM != idN {
		t.Errorf("expected freed slot %d to be reused, got %d", idN, idM)
	}
	if got := len(s.StringIDs()); got != lenBefore {
		t.Errorf("stringIDs grew on reuse: before=%d after=%d", lenBefore, got)
	}
}

// TestReclaim_DoesNotHappen covers every "must NOT reclaim" guard: local node,
// a live member with zero dots, a node referenced by a LIVE dot, and a node
// referenced by a TOMBSTONE dot.
func TestReclaim_DoesNotHappen(t *testing.T) {
	t.Run("local node never reclaimed", func(t *testing.T) {
		s := NewState("node-local")
		// localNode has zero dots and is not in alive — must still be kept.
		if n := s.ReclaimUnreferencedNodes(map[string]struct{}{}); n != 0 {
			t.Fatalf("local node reclaimed: %d", n)
		}
		if s.NodeString(s.LocalNode()) != "node-local" {
			t.Errorf("local node string lost")
		}
	})

	t.Run("live member with zero dots kept", func(t *testing.T) {
		s := NewState("node-local")
		id := s.internNode("node-A") // interned, no dots
		alive := map[string]struct{}{"node-A": {}}
		if n := s.ReclaimUnreferencedNodes(alive); n != 0 {
			t.Fatalf("live member reclaimed: %d", n)
		}
		if s.NodeString(id) != "node-A" {
			t.Errorf("live member string lost")
		}
	})

	t.Run("live dot blocks reclaim", func(t *testing.T) {
		s := NewState("node-local")
		id := s.internNode("node-A")
		e := &Entry{Name: "x", PID: makePID("node-A", "h", "1"), Node: id, Counter: 1, Wall: 10}
		s.Apply(e)
		// node-A not alive, but a live dot references it.
		if n := s.ReclaimUnreferencedNodes(map[string]struct{}{}); n != 0 {
			t.Fatalf("reclaimed an id with a live dot: %d", n)
		}
		if s.NodeString(id) != "node-A" {
			t.Errorf("string lost while live dot present")
		}
	})

	t.Run("tombstone dot blocks reclaim", func(t *testing.T) {
		s := NewState("node-local")
		id := s.internNode("node-A")
		e := &Entry{Name: "x", PID: makePID("node-A", "h", "1"), Node: id, Counter: 1, Wall: 10}
		s.Apply(e)
		s.ReapNode("node-A") // tombstone in place, dot still present
		if n := s.ReclaimUnreferencedNodes(map[string]struct{}{}); n != 0 {
			t.Fatalf("reclaimed an id with a tombstone dot: %d", n)
		}
		if s.NodeString(id) != "node-A" {
			t.Errorf("string lost while tombstone dot present")
		}
	})
}

// TestReclaim_NoCorruptionAfterReuse proves a reused slot resolves to the new
// origin, and a late stale dot re-interning the old origin gets a distinct slot
// (never silently aliased onto the new owner).
func TestReclaim_NoCorruptionAfterReuse(t *testing.T) {
	s := NewState("node-local")
	idN := s.internNode("node-N")
	e := &Entry{Name: "x", PID: makePID("node-N", "h", "1"), Node: idN, Counter: 1, Wall: 10}
	s.Apply(e)
	s.ReapNode("node-N")
	safe := s.CVSnapshot()
	for i := range safe {
		safe[i] = 1 << 62
	}
	s.ReapTombstones(safe, 1_000_000, 0)
	if n := s.ReclaimUnreferencedNodes(map[string]struct{}{"node-local": {}}); n != 1 {
		t.Fatalf("expected reclaim, got %d", n)
	}

	// Reuse slot for node-M.
	idM := s.internNode("node-M")
	if idM != idN {
		t.Fatalf("expected reuse of slot %d, got %d", idN, idM)
	}
	mPID := makePID("node-M", "h", "m1")
	s.Apply(&Entry{Name: "y", PID: mPID, Node: idM, Counter: 1, Wall: 20})
	if s.NodeString(idM) != "node-M" {
		t.Fatalf("reused slot must resolve to node-M, got %q", s.NodeString(idM))
	}
	if got, ok := s.Lookup("y"); !ok || got != mPID {
		t.Fatalf("node-M binding wrong after reuse: ok=%v got=%v", ok, got)
	}

	// A late stale dot from node-N must intern to a FRESH, distinct slot.
	idNagain := s.internNode("node-N")
	if idNagain == idM {
		t.Fatalf("late node-N aliased onto node-M's slot %d", idM)
	}
	if s.NodeString(idM) != "node-M" || s.NodeString(idNagain) != "node-N" {
		t.Fatalf("aliasing: m=%q n=%q", s.NodeString(idM), s.NodeString(idNagain))
	}
}

// TestReclaim_ConvergenceUnaffected proves reclamation is local-only: a replica
// that underwent reclaim+reuse cycles has byte-identical shard hashes to one
// that did not, given the same logical set of dots. Hashes are string-based, so
// compact-id churn cannot change them.
func TestReclaim_ConvergenceUnaffected(t *testing.T) {
	// churned: interns and reclaims throwaway origins before holding the real set.
	churned := NewState("node-home")
	plain := NewState("node-home")

	// Build the real logical dot set on both, applied identically.
	apply := func(s *State, name, origin string, counter uint64, wall int64, del bool) {
		id := s.internNode(origin)
		e := &Entry{Name: name, Node: id, Counter: counter, Wall: wall, Deleted: del}
		if !del {
			e.PID = makePID(origin, "h", fmt.Sprintf("%s-%d", name, counter))
		}
		s.Apply(e)
	}

	// Churn many ephemeral origins on `churned`, fully reap+reclaim them, so its
	// compact-id assignment diverges from `plain` before the real set lands.
	for i := 0; i < 30; i++ {
		o := fmt.Sprintf("ephemeral-%d", i)
		id := churned.internNode(o)
		churned.Apply(&Entry{Name: "tmp", PID: makePID(o, "h", "t"), Node: id, Counter: 1, Wall: 1})
		churned.ReapNode(o)
		safe := churned.CVSnapshot()
		for j := range safe {
			safe[j] = 1 << 62
		}
		churned.ReapTombstones(safe, 1_000_000, 0)
		churned.ReclaimUnreferencedNodes(map[string]struct{}{"node-home": {}})
	}

	type op struct {
		name, origin string
		counter      uint64
		wall         int64
		del          bool
	}
	ops := []op{
		{"alpha", "node-A", 1, 10, false},
		{"beta", "node-B", 1, 11, false},
		{"alpha", "node-B", 1, 12, false}, // cross-origin conflict on alpha
		{"gamma", "node-C", 3, 13, false},
		{"beta", "node-B", 2, 14, true}, // tombstone beta
		{"delta", "node-A", 5, 15, false},
	}
	for _, s := range []*State{churned, plain} {
		for _, o := range ops {
			apply(s, o.name, o.origin, o.counter, o.wall, o.del)
		}
	}

	for i := 0; i < ShardCount; i++ {
		if churned.ShardHash(i) != plain.ShardHash(i) {
			t.Fatalf("shard %d diverged after reclaim churn: churned=%x plain=%x",
				i, churned.ShardHash(i), plain.ShardHash(i))
		}
	}
}

// TestReclaim_BoundedGrowth proves the intern table is bounded by the concurrent
// high-water-mark, not the all-time distinct count: register dots from N
// departed origins, GC them all, then register N new origins — len(stringIDs)
// stays ~N rather than ~2N.
func TestReclaim_BoundedGrowth(t *testing.T) {
	s := NewState("node-local")
	const N = 50

	reapAll := func() {
		safe := s.CVSnapshot()
		for i := range safe {
			safe[i] = 1 << 62
		}
		s.ReapTombstones(safe, 1_000_000, 0)
		s.ReclaimUnreferencedNodes(map[string]struct{}{"node-local": {}})
	}

	for i := 0; i < N; i++ {
		o := fmt.Sprintf("departed-%d", i)
		id := s.internNode(o)
		s.Apply(&Entry{Name: fmt.Sprintf("n%d", i), PID: makePID(o, "h", "p"), Node: id, Counter: 1, Wall: 1})
		s.ReapNode(o)
	}
	reapAll()
	lenAfterFirst := len(s.StringIDs())

	for i := 0; i < N; i++ {
		o := fmt.Sprintf("fresh-%d", i)
		id := s.internNode(o)
		s.Apply(&Entry{Name: fmt.Sprintf("m%d", i), PID: makePID(o, "h", "p"), Node: id, Counter: 1, Wall: 1})
	}
	lenAfterSecond := len(s.StringIDs())

	if lenAfterSecond > lenAfterFirst {
		t.Fatalf("intern table grew on reuse: first=%d second=%d (want bounded)", lenAfterFirst, lenAfterSecond)
	}
	// Sanity: it really is ~N (local + N), not ~2N.
	if lenAfterSecond > N+5 {
		t.Fatalf("intern table not bounded to high-water-mark: len=%d want ~%d", lenAfterSecond, N+1)
	}
}

// TestReclaim_RaceConcurrentRegisterApplyVsReclaim runs Register/Apply against a
// concurrent reclaim cycle under -race and asserts the refcount invariant still
// holds afterward (no premature reclaim, no torn count). Run with -race.
func TestReclaim_RaceConcurrentRegisterApplyVsReclaim(t *testing.T) {
	s := NewState("node-local")
	origins := []string{"node-A", "node-B", "node-C"}
	ids := map[string]uint32{}
	for _, o := range origins {
		ids[o] = s.internNode(o)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writers: local register/unregister + remote applies.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(w) + 1))
			var c uint64
			for {
				select {
				case <-stop:
					return
				default:
				}
				name := fmt.Sprintf("k%d", rng.Intn(20))
				switch rng.Intn(4) {
				case 0:
					s.Register(name, makePID("node-local", "h", "p"), 1, 0)
				case 1:
					s.Unregister(name, 2)
				default:
					o := origins[rng.Intn(len(origins))]
					c++
					s.Apply(&Entry{Name: name, PID: makePID(o, "h", "p"), Node: ids[o], Counter: c, Wall: 1})
				}
			}
		}(w)
	}

	// Reclaimer: reap tombstones then reclaim, with node-A always alive.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			select {
			case <-stop:
				return
			default:
			}
			safe := s.CVSnapshot()
			for j := range safe {
				safe[j] = 1 << 62
			}
			s.ReapTombstones(safe, 1_000_000, 0)
			s.ReclaimUnreferencedNodes(map[string]struct{}{"node-local": {}, "node-A": {}})
		}
	}()

	// Let it run briefly then stop.
	for i := 0; i < 5000; i++ {
		_ = s.LiveCount()
	}
	close(stop)
	wg.Wait()

	// After quiescence the invariant must hold exactly.
	assertRefsConsistent(t, s)
}
