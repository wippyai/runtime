// SPDX-License-Identifier: MPL-2.0

package eventualreg

import (
	"context"
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/pid"
)

func makePID(node, host, uid string) pid.PID {
	return pid.PID{Node: node, Host: host, UniqID: uid}
}

// reg is a test convenience over State.Register at default priority,
// returning the applied entry and whether the local registration won.
func reg(s *State, name string, p pid.PID, wall int64) (*Entry, bool) {
	res := s.Register(name, p, wall, 0)
	return res.Entry, res.Won
}

func TestState_RegisterAndLookup(t *testing.T) {
	s := NewState("node-A")
	p := makePID("node-A", "h1", "p1")
	e, ok := reg(s, "alice", p, 100)
	if !ok {
		t.Fatalf("register failed")
	}
	if e.Counter != 1 {
		t.Errorf("expected counter 1, got %d", e.Counter)
	}
	if got, found := s.Lookup("alice"); !found || got != p {
		t.Errorf("lookup: got=%v found=%v", got, found)
	}
}

func TestState_RegisterRejectsDifferentPID(t *testing.T) {
	s := NewState("node-A")
	p1 := makePID("node-A", "h1", "p1")
	p2 := makePID("node-A", "h1", "p2")
	if _, ok := reg(s, "alice", p1, 100); !ok {
		t.Fatalf("first register failed")
	}
	if _, ok := reg(s, "alice", p2, 200); ok {
		t.Errorf("expected rejection for different PID")
	}
}

func TestState_UnregisterTombstones(t *testing.T) {
	s := NewState("node-A")
	p := makePID("node-A", "h1", "p1")
	_, _ = reg(s, "alice", p, 100)
	tomb := s.Unregister("alice", 200)
	if tomb == nil {
		t.Fatalf("expected tombstone")
	}
	if !tomb.Deleted {
		t.Errorf("expected Deleted=true")
	}
	if _, found := s.Lookup("alice"); found {
		t.Errorf("expected lookup miss after unregister")
	}
	if s.LiveCount() != 0 {
		t.Errorf("LiveCount=%d, want 0", s.LiveCount())
	}
	if s.TombstoneCount() != 1 {
		t.Errorf("TombstoneCount=%d, want 1", s.TombstoneCount())
	}
}

// TestState_ConcurrentResolutionIgnoresWall proves the resolver does NOT use
// wall clock for cross-origin conflicts: the winner is fixed by the
// (Priority, FNV64(name,origin)) key regardless of which entry has the higher
// wall. A later same-origin counter does not flip the cross-origin rank.
func TestState_ConcurrentResolutionIgnoresWall(t *testing.T) {
	s := NewState("node-A")
	pA := makePID("node-A", "h1", "p1")
	pB := makePID("node-B", "h1", "p2")

	originA := s.LocalNode()
	originB := s.internNode("node-B")

	// Determine the deterministic winner up front from the key.
	keyA := s.winnerKey(&Entry{Name: "x", Node: originA})
	keyB := s.winnerKey(&Entry{Name: "x", Node: originB})
	aWins := concurrentKeyCmp(keyA, keyB) > 0

	// Apply A then B; B has a much higher wall, which must be irrelevant.
	e1 := &Entry{Name: "x", PID: pA, Node: originA, Counter: 1, Wall: 100}
	e2 := &Entry{Name: "x", PID: pB, Node: originB, Counter: 5, Wall: 9999}
	if outcome, _, _ := s.Apply(e1); outcome != MergeApplied {
		t.Errorf("first apply outcome=%d", outcome)
	}
	s.Apply(e2)

	got, _ := s.Lookup("x")
	wantPID := pB
	if aWins {
		wantPID = pA
	}
	if got != wantPID {
		t.Errorf("deterministic winner mismatch: got %v want %v (aWins=%v)", got, wantPID, aWins)
	}

	// A later same-origin counter from B must not change the cross-origin rank.
	e3 := &Entry{Name: "x", PID: pB, Node: originB, Counter: 6, Wall: 1}
	s.Apply(e3)
	got2, _ := s.Lookup("x")
	if aWins && got2 != pA {
		t.Errorf("rank flipped as counter advanced: got %v", got2)
	}
	if !aWins && (got2.Node != "node-B") {
		t.Errorf("expected B to remain winner with newer counter, got %v", got2)
	}
}

func TestState_MergeSameOriginHigherCounterWins(t *testing.T) {
	s := NewState("node-A")
	originB := s.internNode("node-B")

	e1 := &Entry{Name: "x", PID: makePID("node-B", "h", "1"), Node: originB, Counter: 1, Wall: 100}
	e2 := &Entry{Name: "x", PID: makePID("node-B", "h", "2"), Node: originB, Counter: 2, Wall: 50}

	dot := func() *Entry {
		sh := &s.shards[ShardFor("x")]
		sh.mu.RLock()
		defer sh.mu.RUnlock()
		return sh.entries["x"].dots[originB]
	}
	if s.Apply(e1); dot().Counter != 1 {
		t.Errorf("e1 not applied")
	}
	if outcome, _, _ := s.Apply(e2); outcome != MergeApplied {
		t.Errorf("expected MergeApplied (higher same-origin counter); got %d", outcome)
	}
	if dot().Counter != 2 {
		t.Errorf("e2 not applied")
	}
}

func TestState_MergeDeleteWinsOnEqualDot(t *testing.T) {
	s := NewState("node-A")
	originB := s.internNode("node-B")

	live := &Entry{Name: "x", PID: makePID("node-B", "h", "1"), Node: originB, Counter: 7, Wall: 100}
	tomb := &Entry{Name: "x", Node: originB, Counter: 7, Wall: 100, Deleted: true}

	s.Apply(live)
	if outcome, _, _ := s.Apply(tomb); outcome != MergeDeleteWins {
		t.Errorf("expected MergeDeleteWins; got %d", outcome)
	}
	if _, found := s.Lookup("x"); found {
		t.Errorf("tombstone not visible")
	}
}

// TestState_ReapplyWinnerIsNoopNoLost proves the revoke dedupe set is redundant:
// re-applying the exact same winning remote dot returns MergeNoop with no
// LostBinding on every replay. The first apply (winner change away from the
// local live dot) is the ONLY one that reports the loss; anti-entropy replays of
// the identical dot are noops. This is the evidence that emittedRevokes can be
// removed without re-emitting under anti-entropy.
func TestState_ReapplyWinnerIsNoopNoLost(t *testing.T) {
	s := NewState("node-A")
	localPID := makePID("node-A", "h", "local")
	res := s.Register("svc.leader", localPID, 1000, 0)
	if !res.Won {
		t.Fatalf("local register should win first")
	}

	originB := s.internNode("node-B")
	winner := &Entry{
		Name:     "svc.leader",
		PID:      makePID("node-B", "h", "remote"),
		Node:     originB,
		Counter:  1,
		Wall:     1000,
		Priority: 10,
	}

	outcome, _, lost := s.Apply(winner)
	if lost == nil {
		t.Fatalf("first apply of higher-priority remote must report the local loss")
	}
	if outcome == MergeNoop {
		t.Fatalf("first apply must change the winner, got MergeNoop")
	}

	for i := 0; i < 3; i++ {
		outcome, _, lost = s.Apply(winner)
		if outcome != MergeNoop {
			t.Errorf("replay %d: expected MergeNoop, got %d", i, outcome)
		}
		if lost != nil {
			t.Errorf("replay %d: expected no LostBinding on re-application", i)
		}
	}
}

// TestState_ReapNodeTombstonesForeignOrigin proves ReapNode tombstones a dot
// whose origin is the departed node (not the local node) in place, keeping its
// (origin, counter) and only flipping Deleted, and returns it for broadcast.
// Unregister cannot do this — it only touches rec.dots[localNode].
func TestState_ReapNodeTombstonesForeignOrigin(t *testing.T) {
	s := NewState("node-A")
	originB := s.internNode("node-B")
	pOnB := makePID("node-B", "h", "b1")

	live := &Entry{Name: "x", PID: pOnB, Node: originB, Counter: 7, Wall: 100}
	s.Apply(live)
	if _, found := s.Lookup("x"); !found {
		t.Fatalf("setup: x must be live")
	}

	// Unregister is a no-op: there is no local-origin dot for x.
	if tomb := s.Unregister("x", 200); tomb != nil {
		t.Fatalf("Unregister must not touch the foreign-origin dot, got %+v", tomb)
	}
	if _, found := s.Lookup("x"); !found {
		t.Fatalf("Unregister must leave the foreign-origin binding live (the bug)")
	}

	out := s.ReapNode("node-B")
	if len(out) != 1 {
		t.Fatalf("ReapNode should return exactly one tombstone, got %d", len(out))
	}
	tomb := out[0]
	if !tomb.Deleted || tomb.Node != originB || tomb.Counter != 7 {
		t.Errorf("tombstone must keep (origin=node-B, counter=7) and set Deleted; got %+v", tomb)
	}
	if _, found := s.Lookup("x"); found {
		t.Errorf("x must be tombstoned after ReapNode")
	}
	if s.TombstoneCount() != 1 || s.LiveCount() != 0 {
		t.Errorf("counts: live=%d tombstone=%d, want 0/1", s.LiveCount(), s.TombstoneCount())
	}
}

// TestState_ReapNodeConverges proves two replicas that both hold the departed
// node's remote dot, each running ReapNode independently, produce the identical
// same-dot tombstone and converge (equal shard hashes), with delete-wins folding
// the tombstone over any lingering live copy exchanged afterwards.
func TestState_ReapNodeConverges(t *testing.T) {
	a := NewState("node-A")
	c := NewState("node-C")

	pOnB := makePID("node-B", "h", "b1")
	originBa := a.internNode("node-B")
	originBc := c.internNode("node-B")

	liveA := &Entry{Name: "sess", PID: pOnB, Node: originBa, Counter: 4, Wall: 100}
	liveC := &Entry{Name: "sess", PID: pOnB, Node: originBc, Counter: 4, Wall: 100}
	a.Apply(liveA)
	c.Apply(liveC)

	tombsA := a.ReapNode("node-B")
	tombsC := c.ReapNode("node-B")
	if len(tombsA) != 1 || len(tombsC) != 1 {
		t.Fatalf("each replica must produce one tombstone: a=%d c=%d", len(tombsA), len(tombsC))
	}

	// Identical same-dot tombstone (same origin string, counter, deleted).
	if tombsA[0].Counter != tombsC[0].Counter || !tombsA[0].Deleted || !tombsC[0].Deleted {
		t.Errorf("tombstones diverge: a=%+v c=%+v", tombsA[0], tombsC[0])
	}

	// Cross-apply each other's tombstone: delete-wins keeps them converged.
	ta := *tombsA[0]
	tc := *tombsC[0]
	ta.Node = originBc // re-intern for c's table
	tc.Node = originBa // re-intern for a's table
	c.Apply(&ta)
	a.Apply(&tc)

	if _, found := a.Lookup("sess"); found {
		t.Errorf("a must see sess tombstoned")
	}
	if _, found := c.Lookup("sess"); found {
		t.Errorf("c must see sess tombstoned")
	}
	for i := 0; i < ShardCount; i++ {
		if a.ShardHash(i) != c.ShardHash(i) {
			t.Errorf("shard %d diverged after ReapNode convergence", i)
		}
	}
}

func TestState_ShardHashDeterministic(t *testing.T) {
	a := NewState("node-A")
	b := NewState("node-B")
	originX := a.internNode("node-X")
	originXb := b.internNode("node-X")

	for i := 0; i < 50; i++ {
		name := fmt.Sprintf("name-%d", i)
		e := &Entry{
			Name:    name,
			PID:     makePID("node-X", "h", fmt.Sprintf("p%d", i)),
			Counter: uint64(i + 1),
			Wall:    int64(i * 10),
		}
		ea := *e
		eb := *e
		ea.Node = originX
		eb.Node = originXb
		a.Apply(&ea)
		b.Apply(&eb)
	}

	for i := 0; i < ShardCount; i++ {
		ah := a.ShardHash(i)
		bh := b.ShardHash(i)
		if ah != bh {
			t.Errorf("shard %d hash diverged: a=%x b=%x", i, ah, bh)
		}
	}
}

func TestState_ShardHashIncludesPID(t *testing.T) {
	// Two replicas hold the same name with the same (origin, counter, wall,
	// deleted) dot but DIFFERENT resolved PIDs. The shard hash must differ so
	// digest diff fires anti-entropy and the PID disagreement reconciles.
	a := NewState("node-A")
	b := NewState("node-B")
	originXa := a.internNode("node-X")
	originXb := b.internNode("node-X")

	const name = "session-1"
	base := Entry{
		Name:    name,
		Counter: 7,
		Wall:    1234,
	}
	ea := base
	ea.Node = originXa
	ea.PID = makePID("node-X", "host-1", "uniq-A")

	eb := base
	eb.Node = originXb
	eb.PID = makePID("node-X", "host-1", "uniq-B")

	a.Apply(&ea)
	b.Apply(&eb)

	idx := ShardFor(name)
	if got := a.ShardHash(idx); got == b.ShardHash(idx) {
		t.Fatalf("shard hashes must differ for divergent PIDs; both=%x", got)
	}
}

func TestState_ShardHashStableForSamePID(t *testing.T) {
	// Identical name + identical PID + identical meta on two independently
	// built replicas must yield identical hashes — the digest's premise.
	a := NewState("node-A")
	b := NewState("node-B")
	originXa := a.internNode("node-X")
	originXb := b.internNode("node-X")

	const name = "session-1"
	base := Entry{
		Name:    name,
		Counter: 7,
		Wall:    1234,
		PID:     makePID("node-X", "host-1", "uniq-same"),
	}
	ea := base
	ea.Node = originXa
	eb := base
	eb.Node = originXb

	a.Apply(&ea)
	b.Apply(&eb)

	idx := ShardFor(name)
	if a.ShardHash(idx) != b.ShardHash(idx) {
		t.Fatalf("shard hashes must match for identical entries: a=%x b=%x",
			a.ShardHash(idx), b.ShardHash(idx))
	}
}

func TestState_ReapTombstones_SafeCounter(t *testing.T) {
	s := NewState("node-A")
	pA := makePID("node-A", "h", "1")
	_, _ = reg(s, "alice", pA, 100)
	_ = s.Unregister("alice", 200)

	if s.TombstoneCount() != 1 {
		t.Fatalf("expected 1 tombstone")
	}

	// Local origin is 0; counter for tombstone is 2 (register+unregister).
	// Safe counter at 5 means peer has acked through counter 5.
	cv := make([]uint64, 1)
	cv[0] = 5
	gcSafe, gcFloor := s.ReapTombstones(cv, 1000, 60_000)
	if gcSafe != 1 {
		t.Errorf("gcSafe=%d, want 1", gcSafe)
	}
	if gcFloor != 0 {
		t.Errorf("gcFloor=%d, want 0", gcFloor)
	}
	if s.TombstoneCount() != 0 {
		t.Errorf("tombstone not reaped")
	}
}

func TestState_ReapTombstones_WallFloor(t *testing.T) {
	s := NewState("node-A")
	pA := makePID("node-A", "h", "1")
	_, _ = reg(s, "alice", pA, 100)
	_ = s.Unregister("alice", 200)

	// No safe counter — peers haven't acked. Wall floor catches it.
	cv := make([]uint64, 1)
	cv[0] = 0
	gcSafe, gcFloor := s.ReapTombstones(cv, 1_000_000_000, 60_000) // 1B ms wall, 60s floor
	if gcSafe != 0 {
		t.Errorf("gcSafe=%d, want 0", gcSafe)
	}
	if gcFloor != 1 {
		t.Errorf("gcFloor=%d, want 1", gcFloor)
	}
	if s.TombstoneCount() != 0 {
		t.Errorf("tombstone not reaped")
	}
}

func TestState_DeltaRoundTrip(t *testing.T) {
	s := NewState("node-A")
	pA := makePID("node-A", "h", "1")
	e, _ := reg(s, "alice", pA, 12345)

	originStr := s.NodeString(e.Node)
	if originStr != "node-A" {
		t.Fatalf("origin string mismatch: %q", originStr)
	}

	buf, err := EncodeDelta(nil, e, originStr)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	got, gotOrigin, n, err := DecodeDelta(buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if n != len(buf) {
		t.Errorf("consumed %d, want %d", n, len(buf))
	}
	if gotOrigin != "node-A" {
		t.Errorf("origin: %q", gotOrigin)
	}
	if got.Name != "alice" || got.PID != pA || got.Counter != e.Counter || got.Wall != 12345 {
		t.Errorf("entry mismatch: %+v", got)
	}
}

// recordingSender captures every Send() call so a test can assert what
// the shard-pull path emitted and replay it into the peer.
type recordingSender struct {
	peer *Service
	sent []struct {
		To      string
		Payload []byte
	}
}

func (r *recordingSender) Send(target string, payload []byte) error {
	r.sent = append(r.sent, struct {
		To      string
		Payload []byte
	}{To: target, Payload: append([]byte(nil), payload...)})
	if r.peer != nil {
		r.peer.OnFrame(payload)
	}
	return nil
}

// TestService_ShardPullRecoversDroppedDelta exercises the WS-D path:
// A has alice + bob, B has only alice (a delta was dropped). B's
// MergeRemoteState detects the digest mismatch and emits a shard-pull
// request to A; A's OnFrame builds a response with the missing shards;
// B's OnFrame merges, ending with both names.
func TestService_ShardPullRecoversDroppedDelta(t *testing.T) {
	cfgA := Config{LocalNodeID: "node-A"}
	cfgB := Config{LocalNodeID: "node-B"}
	a := NewService(cfgA)
	b := NewService(cfgB)
	t.Cleanup(func() { _ = a.Stop(); _ = b.Stop() })

	// Cross-wire senders so each service can ship to the other.
	senderToA := &recordingSender{peer: a}
	senderToB := &recordingSender{peer: b}
	a.cfg.Sender = senderToB
	b.cfg.Sender = senderToA

	// A registers alice + bob; B is empty — the "dropped delta"
	// equivalent for both entries.
	pAlice := makePID("node-A", "h", "alice-pid")
	pBob := makePID("node-A", "h", "bob-pid")
	if _, err := a.Register("alice", pAlice); err != nil {
		t.Fatalf("register alice on A: %v", err)
	}
	if _, err := a.Register("bob", pBob); err != nil {
		t.Fatalf("register bob on A: %v", err)
	}

	// Sanity: digests differ since B is empty.
	mismatched := b.LocalDigest().Diff(a.LocalDigest())
	if len(mismatched) == 0 {
		t.Fatalf("expected digest mismatch with empty B")
	}

	// Simulate B's MergeRemoteState path emitting the shard request.
	if !b.RequestShards("node-A", mismatched) {
		t.Fatalf("RequestShards should have emitted")
	}

	// After the synchronous round trip, B must now hold both names.
	if res, err := b.Lookup(context.Background(), "alice"); err != nil || !res.Found || res.PID != pAlice {
		t.Fatalf("alice not recovered on B: err=%v found=%v got=%v", err, res.Found, res.PID)
	}
	if res, err := b.Lookup(context.Background(), "bob"); err != nil || !res.Found || res.PID != pBob {
		t.Fatalf("bob not recovered on B: err=%v found=%v got=%v", err, res.Found, res.PID)
	}

	// Cooldown: a second immediate request must be suppressed.
	if b.RequestShards("node-A", mismatched) {
		t.Errorf("cooldown should have suppressed second request")
	}
}

func TestState_Convergence_TwoReplicas(t *testing.T) {
	a := NewState("node-A")
	b := NewState("node-B")

	pA := makePID("node-A", "h", "p1")
	pB := makePID("node-B", "h", "p2")

	// A registers alice; B registers bob.
	eA, _ := reg(a, "alice", pA, 100)
	eB, _ := reg(b, "bob", pB, 200)

	originA := b.internNode("node-A")
	originB := a.internNode("node-B")
	eAcopy := *eA
	eBcopy := *eB
	eAcopy.Node = originA
	eBcopy.Node = originB

	// Cross-apply.
	b.Apply(&eAcopy)
	a.Apply(&eBcopy)

	// Both replicas should see both names.
	for _, s := range []*State{a, b} {
		if got, ok := s.Lookup("alice"); !ok || got != pA {
			t.Errorf("alice not converged: ok=%v got=%v", ok, got)
		}
		if got, ok := s.Lookup("bob"); !ok || got != pB {
			t.Errorf("bob not converged: ok=%v got=%v", ok, got)
		}
	}
	// Shard hashes must agree.
	for i := 0; i < ShardCount; i++ {
		if a.ShardHash(i) != b.ShardHash(i) {
			t.Errorf("shard %d diverged", i)
		}
	}
}
