// SPDX-License-Identifier: MPL-2.0

package crdt

import (
	"bytes"
	"fmt"
	"testing"
)

func TestState_RegisterAndLookup(t *testing.T) {
	s := NewState("node-A")
	val := []byte("hello")
	e, ok := s.Register("alice", val, 100)
	if !ok {
		t.Fatalf("register failed")
	}
	if e.Counter != 1 {
		t.Errorf("counter=%d want 1", e.Counter)
	}
	got, found := s.Lookup("alice")
	if !found {
		t.Errorf("not found")
	}
	if !bytes.Equal(got, val) {
		t.Errorf("got %q want %q", got, val)
	}
}

func TestState_OverwriteReplaces(t *testing.T) {
	s := NewState("node-A")
	_, _ = s.Register("k", []byte("v1"), 100)
	e := s.Overwrite("k", []byte("v2"), 200)
	if e.Counter != 2 {
		t.Errorf("counter=%d want 2", e.Counter)
	}
	got, _ := s.Lookup("k")
	if string(got) != "v2" {
		t.Errorf("got %q want v2", got)
	}
}

func TestState_RegisterRejectsDifferentValue(t *testing.T) {
	s := NewState("node-A")
	if _, ok := s.Register("k", []byte("v1"), 100); !ok {
		t.Fatalf("first register failed")
	}
	if _, ok := s.Register("k", []byte("v2"), 200); ok {
		t.Errorf("expected rejection for different value")
	}
}

func TestState_RegisterAcceptsSameValue(t *testing.T) {
	s := NewState("node-A")
	if _, ok := s.Register("k", []byte("v"), 100); !ok {
		t.Fatalf("first register failed")
	}
	if _, ok := s.Register("k", []byte("v"), 200); !ok {
		t.Errorf("re-register with same value should succeed")
	}
}

func TestState_UnregisterTombstones(t *testing.T) {
	s := NewState("node-A")
	_, _ = s.Register("k", []byte("v"), 100)
	tomb, ok := s.Unregister("k", 200)
	if !ok || !tomb.Deleted {
		t.Fatalf("expected tombstone")
	}
	if _, found := s.Lookup("k"); found {
		t.Errorf("lookup should miss after unregister")
	}
	if s.LiveCount() != 0 || s.TombstoneCount() != 1 {
		t.Errorf("live=%d tomb=%d", s.LiveCount(), s.TombstoneCount())
	}
}

func TestState_MergeHigherCounterWins(t *testing.T) {
	s := NewState("node-A")
	bID := s.InternNode("node-B")

	e1 := Entry{Key: "k", Value: []byte("from-A"), Node: s.LocalNode(), Counter: 1, Wall: 100}
	e2 := Entry{Key: "k", Value: []byte("from-B"), Node: bID, Counter: 5, Wall: 50}

	if outcome, _ := s.Apply(e1); outcome != MergeApplied {
		t.Errorf("first apply outcome=%d", outcome)
	}
	// Different origin → concurrent → wall LWW wins. e1 wall=100 > e2 wall=50.
	if outcome, _ := s.Apply(e2); outcome != MergeNoop {
		t.Errorf("expected MergeNoop (e1 wall higher); got %d", outcome)
	}
	got, _ := s.Lookup("k")
	if string(got) != "from-A" {
		t.Errorf("expected from-A, got %q", got)
	}
}

func TestState_MergeDeleteWinsOnEqualDot(t *testing.T) {
	s := NewState("node-A")
	bID := s.InternNode("node-B")

	live := Entry{Key: "k", Value: []byte("v"), Node: bID, Counter: 7, Wall: 100}
	tomb := Entry{Key: "k", Node: bID, Counter: 7, Wall: 100, Deleted: true}

	_, _ = s.Apply(live)
	if outcome, _ := s.Apply(tomb); outcome != MergeDeleteWins {
		t.Errorf("expected MergeDeleteWins; got %d", outcome)
	}
	if _, found := s.Lookup("k"); found {
		t.Errorf("tombstone not visible")
	}
}

func TestState_ShardHashDeterministic(t *testing.T) {
	a := NewState("node-A")
	b := NewState("node-B")
	originXa := a.InternNode("node-X")
	originXb := b.InternNode("node-X")

	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key-%d", i)
		val := []byte(fmt.Sprintf("val-%d", i))
		ea := Entry{Key: key, Value: val, Counter: uint64(i + 1), Wall: int64(i * 10), Node: originXa}
		eb := Entry{Key: key, Value: val, Counter: uint64(i + 1), Wall: int64(i * 10), Node: originXb}
		_, _ = a.Apply(ea)
		_, _ = b.Apply(eb)
	}

	for i := 0; i < ShardCount; i++ {
		ah := a.ShardHash(i)
		bh := b.ShardHash(i)
		if ah != bh {
			t.Errorf("shard %d diverged: a=%x b=%x", i, ah, bh)
		}
	}
}

func TestState_ConvergenceTwoReplicas(t *testing.T) {
	a := NewState("node-A")
	b := NewState("node-B")

	eA, _ := a.Register("alice", []byte("from-A"), 100)
	eB, _ := b.Register("bob", []byte("from-B"), 200)

	// Cross-apply with the receiver's compact origin id.
	originAonB := b.InternNode("node-A")
	originBonA := a.InternNode("node-B")
	eAcopy := eA
	eAcopy.Node = originAonB
	eBcopy := eB
	eBcopy.Node = originBonA

	_, _ = b.Apply(eAcopy)
	_, _ = a.Apply(eBcopy)

	for _, s := range []*State{a, b} {
		got, found := s.Lookup("alice")
		if !found || string(got) != "from-A" {
			t.Errorf("alice missing: ok=%v got=%q", found, got)
		}
		got, found = s.Lookup("bob")
		if !found || string(got) != "from-B" {
			t.Errorf("bob missing: ok=%v got=%q", found, got)
		}
	}
	for i := 0; i < ShardCount; i++ {
		if a.ShardHash(i) != b.ShardHash(i) {
			t.Errorf("shard %d diverged", i)
		}
	}
}

func TestState_DeltaRoundTrip(t *testing.T) {
	s := NewState("node-A")
	e, _ := s.Register("k", []byte("hello"), 12345)

	originStr := s.NodeString(e.Node)
	if originStr != "node-A" {
		t.Fatalf("origin mismatch: %q", originStr)
	}

	buf, err := EncodeDelta(nil, &e, originStr)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	got, gotOrigin, n, err := DecodeDelta(buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if n != len(buf) {
		t.Errorf("consumed %d want %d", n, len(buf))
	}
	if gotOrigin != "node-A" || got.Key != "k" || string(got.Value) != "hello" || got.Counter != e.Counter || got.Wall != 12345 {
		t.Errorf("entry mismatch: %+v origin=%q", got, gotOrigin)
	}
}

func TestState_DigestRoundTrip(t *testing.T) {
	s := NewState("node-A")
	for i := 0; i < 20; i++ {
		_, _ = s.Register(fmt.Sprintf("k-%d", i), []byte(fmt.Sprintf("v-%d", i)), int64(i*100))
	}
	d := MakeDigest(s)
	encoded := d.Encode()
	if len(encoded) != DigestSize {
		t.Errorf("digest size %d want %d", len(encoded), DigestSize)
	}
	decoded, err := DecodeDigest(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for i := 0; i < ShardCount; i++ {
		if d.Hashes[i] != decoded.Hashes[i] {
			t.Errorf("shard %d hash mismatch", i)
		}
	}
}

func TestState_DigestDiff(t *testing.T) {
	a := NewState("node-A")
	b := NewState("node-A")
	_, _ = a.Register("k1", []byte("v"), 100)
	_, _ = a.Register("k2", []byte("v"), 100)
	_, _ = b.Register("k1", []byte("v"), 100)
	// k2 only in a — a's shard for k2 differs from b's.

	mismatched := MakeDigest(a).Diff(MakeDigest(b))
	if len(mismatched) == 0 {
		t.Errorf("expected mismatched shard for k2")
	}
}

func TestState_ReapTombstones_SafeCounter(t *testing.T) {
	s := NewState("node-A")
	_, _ = s.Register("k", []byte("v"), 100)
	_, _ = s.Unregister("k", 200)
	if s.TombstoneCount() != 1 {
		t.Fatalf("expected 1 tombstone")
	}

	cv := []uint64{5}
	gcSafe, gcFloor := s.ReapTombstones(cv, 1000, 60_000)
	if gcSafe != 1 || gcFloor != 0 {
		t.Errorf("gcSafe=%d gcFloor=%d", gcSafe, gcFloor)
	}
	if s.TombstoneCount() != 0 {
		t.Errorf("tombstone not reaped")
	}
}

func TestState_RangeOrdered(t *testing.T) {
	s := NewState("node-A")
	keys := []string{"a", "c", "b", "e", "d"}
	for _, k := range keys {
		_, _ = s.Register(k, []byte("v"), 100)
	}

	var visited []string
	s.Range("b", "e", func(e Entry) bool {
		visited = append(visited, e.Key)
		return true
	})
	want := []string{"b", "c", "d"}
	if len(visited) != len(want) {
		t.Errorf("visited=%v want=%v", visited, want)
	}
	for i, k := range want {
		if i >= len(visited) || visited[i] != k {
			t.Errorf("visited[%d]=%q want=%q", i, visited[i], k)
		}
	}
}
