// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/crdt"
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

func TestCRDTEngine_TombstoneGCWaitsForExactPeerAck(t *testing.T) {
	a := newCRDT(t, "node-a")
	b := newCRDT(t, "node-b")
	alive := func() map[string]struct{} {
		return map[string]struct{}{"node-a": {}, "node-b": {}}
	}
	a.SetAlivePeers(alive)
	b.SetAlivePeers(alive)

	if _, err := b.Set("k", []byte("v")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := b.Delete("k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := b.state.TombstoneCount(); got != 1 {
		t.Fatalf("want one tombstone before GC, got %d", got)
	}

	if safe, floor := b.gcTombstones(); safe+floor != 0 {
		t.Fatalf("tombstone reaped before alive peer ack: safe=%d floor=%d", safe, floor)
	}
	if got := b.state.TombstoneCount(); got != 1 {
		t.Fatalf("tombstone count after blocked GC = %d, want 1", got)
	}

	da := NewCRDTDelegate(a)
	db := NewCRDTDelegate(b)
	da.MergeRemoteState(db.LocalState(false), false)
	db.MergeRemoteState(da.LocalState(false), false)

	if safe, floor := b.gcTombstones(); safe != 1 || floor != 0 {
		t.Fatalf("want one ack-safe tombstone reap after peer ack, got safe=%d floor=%d", safe, floor)
	}
	if got := b.state.TombstoneCount(); got != 0 {
		t.Fatalf("tombstone count after acked GC = %d, want 0", got)
	}
	b.mu.Lock()
	ackPeers := len(b.peerTombAck)
	b.mu.Unlock()
	if ackPeers != 0 {
		t.Fatalf("tombstone ack state leaked after reap: peers=%d", ackPeers)
	}
	if _, err := a.Get("k"); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("peer must retain delete semantics after origin GC, got %v", err)
	}
}

func TestCRDTEngine_TombstoneGCRequiresSpecificTombstoneAck(t *testing.T) {
	a := newCRDT(t, "node-a")
	b := newCRDT(t, "node-b")
	alive := func() map[string]struct{} {
		return map[string]struct{}{"node-a": {}, "node-b": {}}
	}
	a.SetAlivePeers(alive)
	b.SetAlivePeers(alive)

	if _, err := b.Set("k", []byte("v")); err != nil {
		t.Fatalf("set k: %v", err)
	}
	if err := b.Delete("k"); err != nil {
		t.Fatalf("delete k: %v", err)
	}
	if _, err := b.Set("other", []byte("later")); err != nil {
		t.Fatalf("set other: %v", err)
	}

	other, ok := b.state.LookupEntry("other")
	if !ok {
		t.Fatalf("missing later entry")
	}
	frame, err := crdt.EncodeDelta(nil, &other, b.state.NodeString(other.Node))
	if err != nil {
		t.Fatalf("encode later entry: %v", err)
	}
	a.OnFrame(frame)
	NewCRDTDelegate(b).MergeRemoteState(NewCRDTDelegate(a).LocalState(false), false)

	if safe, floor := b.gcTombstones(); safe+floor != 0 {
		t.Fatalf("origin-wide progress without the tombstone must not ack GC: safe=%d floor=%d", safe, floor)
	}
	if got := b.state.TombstoneCount(); got != 1 {
		t.Fatalf("tombstone count after non-specific ack = %d, want 1", got)
	}

	NewCRDTDelegate(a).MergeRemoteState(NewCRDTDelegate(b).LocalState(false), false)
	NewCRDTDelegate(b).MergeRemoteState(NewCRDTDelegate(a).LocalState(false), false)
	if safe, floor := b.gcTombstones(); safe != 1 || floor != 0 {
		t.Fatalf("specific tombstone ack should allow GC: safe=%d floor=%d", safe, floor)
	}
}

func TestCRDTEngine_TombstoneGCExcludesRetiredPeerByPolicy(t *testing.T) {
	alive := map[string]struct{}{"node-b": {}, "node-a": {}}
	b := newCRDT(t, "node-b")
	b.SetAlivePeers(func() map[string]struct{} {
		out := make(map[string]struct{}, len(alive))
		for n := range alive {
			out[n] = struct{}{}
		}
		return out
	})

	if _, err := b.Set("k", []byte("v")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := b.Delete("k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if safe, floor := b.gcTombstones(); safe+floor != 0 {
		t.Fatalf("live but unacked peer must block GC: safe=%d floor=%d", safe, floor)
	}

	// The configured peer set may exclude node-a only after its old AP state is
	// retired or fenced from rejoining under the same node ID.
	delete(alive, "node-a")
	if safe, floor := b.gcTombstones(); safe != 1 || floor != 0 {
		t.Fatalf("retired peer must not pin tombstone: safe=%d floor=%d", safe, floor)
	}
	if got := b.state.TombstoneCount(); got != 0 {
		t.Fatalf("tombstone count after retired-peer GC = %d, want 0", got)
	}
}

func TestCRDTEngine_FullStateEnvelopeBackwardsCompatible(t *testing.T) {
	a := newCRDT(t, "node-a")
	b := newCRDT(t, "node-b")
	if _, err := a.Set("legacy", []byte("v")); err != nil {
		t.Fatalf("set: %v", err)
	}

	var legacy []byte
	for shard := 0; shard < crdt.ShardCount; shard++ {
		entries := a.state.ShardEntries(shard)
		for i := range entries {
			buf, err := crdt.EncodeDelta(nil, &entries[i], a.state.NodeString(entries[i].Node))
			if err != nil {
				t.Fatalf("encode legacy delta: %v", err)
			}
			legacy = append(legacy, buf...)
		}
	}

	NewCRDTDelegate(b).MergeRemoteState(legacy, true)
	if got, err := b.Get("legacy"); err != nil || string(got.Value) != "v" {
		t.Fatalf("legacy full state did not merge: got=%+v err=%v", got, err)
	}
}

func TestCRDTEngine_OnFrameAcceptsFullStateEnvelope(t *testing.T) {
	a := newCRDT(t, "node-a")
	b := newCRDT(t, "node-b")
	if _, err := a.Set("enveloped", []byte("v")); err != nil {
		t.Fatalf("set: %v", err)
	}

	b.OnFrame(a.FullState())
	if got, err := b.Get("enveloped"); err != nil || string(got.Value) != "v" {
		t.Fatalf("enveloped full state did not merge through OnFrame: got=%+v err=%v", got, err)
	}
}

func TestCRDTEngine_FullStateEnvelopeConsumesMalformedHeader(t *testing.T) {
	e := newCRDT(t, "node-a")

	buf := append([]byte{}, kvCRDTFullStateMagic[:]...)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len("peer")+1))
	buf = append(buf, "peer"...)

	if !e.mergeFullState(buf) {
		t.Fatalf("new-format malformed envelope should be consumed, not passed to legacy decoder")
	}
	e.mu.Lock()
	_, recorded := e.peerTombAck["peer"]
	e.mu.Unlock()
	if recorded {
		t.Fatalf("malformed envelope recorded peer tombstone ack")
	}
}

func BenchmarkCRDTEngine_FullStateEnvelopeMerge(b *testing.B) {
	src := NewCRDTEngine("node-a", nil, nil)
	dst := NewCRDTEngine("node-b", nil, nil)
	if err := src.Start(context.Background()); err != nil {
		b.Fatalf("start src: %v", err)
	}
	if err := dst.Start(context.Background()); err != nil {
		b.Fatalf("start dst: %v", err)
	}
	b.Cleanup(func() {
		_ = src.Stop()
		_ = dst.Stop()
	})

	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("k-%04d", i)
		if _, err := src.Set(key, []byte("value")); err != nil {
			b.Fatalf("set %s: %v", key, err)
		}
		if i%3 == 0 {
			if err := src.Delete(key); err != nil {
				b.Fatalf("delete %s: %v", key, err)
			}
		}
	}

	delegate := NewCRDTDelegate(dst)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		delegate.MergeRemoteState(src.FullState(), false)
	}
}
