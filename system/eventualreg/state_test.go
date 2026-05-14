// SPDX-License-Identifier: MPL-2.0

package eventualreg

import (
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/pid"
)

func makePID(node, host, uid string) pid.PID {
	return pid.PID{Node: node, Host: host, UniqID: uid}
}

func TestState_RegisterAndLookup(t *testing.T) {
	s := NewState("node-A")
	p := makePID("node-A", "h1", "p1")
	e, ok := s.Register("alice", p, 100)
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
	if _, ok := s.Register("alice", p1, 100); !ok {
		t.Fatalf("first register failed")
	}
	if _, ok := s.Register("alice", p2, 200); ok {
		t.Errorf("expected rejection for different PID")
	}
}

func TestState_UnregisterTombstones(t *testing.T) {
	s := NewState("node-A")
	p := makePID("node-A", "h1", "p1")
	_, _ = s.Register("alice", p, 100)
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

func TestState_MergeHigherCounterWins(t *testing.T) {
	s := NewState("node-A")
	pA := makePID("node-A", "h1", "p1")
	pB := makePID("node-B", "h1", "p2")

	originB := s.internNode("node-B")
	e1 := &Entry{Name: "x", PID: pA, Node: s.LocalNode(), Counter: 1, Wall: 100}
	e2 := &Entry{Name: "x", PID: pB, Node: originB, Counter: 5, Wall: 50}

	if outcome, _ := s.Apply(e1); outcome != MergeApplied {
		t.Errorf("first apply outcome=%d", outcome)
	}
	// Different origin — concurrent — wall-clock LWW. e1 wall=100 > e2 wall=50.
	if outcome, _ := s.Apply(e2); outcome != MergeNoop {
		t.Errorf("expected MergeNoop (e1 wall higher); got %d", outcome)
	}
	if got, _ := s.Lookup("x"); got != pA {
		t.Errorf("expected pA, got %v", got)
	}

	// Now a higher-wall e3 from B should win.
	e3 := &Entry{Name: "x", PID: pB, Node: originB, Counter: 6, Wall: 300}
	if outcome, _ := s.Apply(e3); outcome != MergeWallTiebreak {
		t.Errorf("expected MergeWallTiebreak; got %d", outcome)
	}
	if got, _ := s.Lookup("x"); got != pB {
		t.Errorf("expected pB after wall LWW, got %v", got)
	}
}

func TestState_MergeSameOriginHigherCounterWins(t *testing.T) {
	s := NewState("node-A")
	originB := s.internNode("node-B")

	e1 := &Entry{Name: "x", PID: makePID("node-B", "h", "1"), Node: originB, Counter: 1, Wall: 100}
	e2 := &Entry{Name: "x", PID: makePID("node-B", "h", "2"), Node: originB, Counter: 2, Wall: 50}

	if _, _ = s.Apply(e1); s.shards[ShardFor("x")].entries["x"].Counter != 1 {
		t.Errorf("e1 not applied")
	}
	if outcome, _ := s.Apply(e2); outcome != MergeApplied {
		t.Errorf("expected MergeApplied (higher same-origin counter); got %d", outcome)
	}
	if s.shards[ShardFor("x")].entries["x"].Counter != 2 {
		t.Errorf("e2 not applied")
	}
}

func TestState_MergeDeleteWinsOnEqualDot(t *testing.T) {
	s := NewState("node-A")
	originB := s.internNode("node-B")

	live := &Entry{Name: "x", PID: makePID("node-B", "h", "1"), Node: originB, Counter: 7, Wall: 100}
	tomb := &Entry{Name: "x", Node: originB, Counter: 7, Wall: 100, Deleted: true}

	_, _ = s.Apply(live)
	if outcome, _ := s.Apply(tomb); outcome != MergeDeleteWins {
		t.Errorf("expected MergeDeleteWins; got %d", outcome)
	}
	if _, found := s.Lookup("x"); found {
		t.Errorf("tombstone not visible")
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
		_, _ = a.Apply(&ea)
		_, _ = b.Apply(&eb)
	}

	for i := 0; i < ShardCount; i++ {
		ah := a.ShardHash(i)
		bh := b.ShardHash(i)
		if ah != bh {
			t.Errorf("shard %d hash diverged: a=%x b=%x", i, ah, bh)
		}
	}
}

func TestState_ReapTombstones_SafeCounter(t *testing.T) {
	s := NewState("node-A")
	pA := makePID("node-A", "h", "1")
	_, _ = s.Register("alice", pA, 100)
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
	_, _ = s.Register("alice", pA, 100)
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
	e, _ := s.Register("alice", pA, 12345)

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
	if got, ok := b.Lookup("alice"); !ok || got != pAlice {
		t.Fatalf("alice not recovered on B: ok=%v got=%v", ok, got)
	}
	if got, ok := b.Lookup("bob"); !ok || got != pBob {
		t.Fatalf("bob not recovered on B: ok=%v got=%v", ok, got)
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
	eA, _ := a.Register("alice", pA, 100)
	eB, _ := b.Register("bob", pB, 200)

	originA := b.internNode("node-A")
	originB := a.internNode("node-B")
	eAcopy := *eA
	eBcopy := *eB
	eAcopy.Node = originA
	eBcopy.Node = originB

	// Cross-apply.
	_, _ = b.Apply(&eAcopy)
	_, _ = a.Apply(&eBcopy)

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
