// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"bytes"
	"encoding/gob"
	"io"
	"testing"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestEncodeDecodeTxn(t *testing.T) {
	ops := []kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondExists, Key: "a"},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: "b", Value: []byte("vb")},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondVersion, Key: "c", Expect: 9},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: "", Value: nil},
	}
	got, err := decodeTxn(encodeTxn(ops))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != len(ops) {
		t.Fatalf("len %d != %d", len(got), len(ops))
	}
	for i, op := range ops {
		g := got[i]
		if g.Kind != op.Kind || g.Cond != op.Cond || g.Key != op.Key ||
			string(g.Value) != string(op.Value) || g.Expect != op.Expect {
			t.Fatalf("op %d mismatch: %+v != %+v", i, g, op)
		}
	}
}

func TestRaftEngine_CompareAndDelete(t *testing.T) {
	eng, _ := newEngine(t)

	ver, err := eng.Set("k", []byte("v"))
	if err != nil {
		t.Fatalf("set: %v", err)
	}

	ok, err := eng.CompareAndDelete("k", ver+1)
	if err != nil || ok {
		t.Fatalf("CAD wrong version: ok=%v err=%v (want false,nil)", ok, err)
	}
	if _, err := eng.Get("k"); err != nil {
		t.Fatalf("key must survive a mismatched CAD: %v", err)
	}

	ok, err = eng.CompareAndDelete("k", ver)
	if err != nil || !ok {
		t.Fatalf("CAD matching version: ok=%v err=%v (want true,nil)", ok, err)
	}
	if _, err := eng.Get("k"); err == nil {
		t.Fatalf("key must be gone after matching CAD")
	}

	ok, err = eng.CompareAndDelete("missing", 0)
	if err != nil || ok {
		t.Fatalf("CAD missing key: ok=%v err=%v (want false,nil)", ok, err)
	}
}

func TestRaftEngine_TxnAllOrNothing(t *testing.T) {
	eng, _ := newEngine(t)

	committed, err := eng.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: "x", Value: []byte("1")},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: "y", Value: []byte("2")},
	})
	if err != nil || !committed {
		t.Fatalf("txn commit: committed=%v err=%v", committed, err)
	}
	if _, err := eng.Get("x"); err != nil {
		t.Fatalf("x must exist: %v", err)
	}
	if _, err := eng.Get("y"); err != nil {
		t.Fatalf("y must exist: %v", err)
	}

	committed, err = eng.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: "z", Value: []byte("3")},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: "x", Value: []byte("clobber")},
	})
	if err != nil || committed {
		t.Fatalf("txn must abort on failed precondition: committed=%v err=%v", committed, err)
	}
	if _, err := eng.Get("z"); err == nil {
		t.Fatalf("z must NOT exist after aborted txn")
	}
	if e, _ := eng.Get("x"); string(e.Value) != "1" {
		t.Fatalf("x must be unchanged after aborted txn, got %q", e.Value)
	}
}

// TestRaftEngine_TxnAtomicPromote exercises the pending->active swap pattern the
// strong registry relies on: delete the pending key and create the active key in
// one atomic txn, gated on the pending version.
func TestRaftEngine_TxnAtomicPromote(t *testing.T) {
	eng, _ := newEngine(t)

	pv, err := eng.Set("pending:n", []byte("p"))
	if err != nil {
		t.Fatalf("seed pending: %v", err)
	}

	committed, err := eng.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: "pending:n", Expect: pv},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: "pending:n"},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: "active:n", Value: []byte("a")},
	})
	if err != nil || !committed {
		t.Fatalf("promote txn: committed=%v err=%v", committed, err)
	}
	if _, err := eng.Get("pending:n"); err == nil {
		t.Fatalf("pending key must be gone after promote")
	}
	if e, err := eng.Get("active:n"); err != nil || string(e.Value) != "a" {
		t.Fatalf("active key must exist after promote: %v %q", err, e.Value)
	}

	committed, err = eng.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: "pending:n", Expect: pv},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: "active:n", Value: []byte("stale")},
	})
	if err != nil || committed {
		t.Fatalf("stale-version promote must abort: committed=%v err=%v", committed, err)
	}
	if e, _ := eng.Get("active:n"); string(e.Value) != "a" {
		t.Fatalf("active must be unchanged after stale abort, got %q", e.Value)
	}
}

func TestRaftEngine_EpochStampsRaftIndex(t *testing.T) {
	eng, _ := newEngine(t)

	if _, err := eng.Set("a", []byte("1")); err != nil {
		t.Fatalf("set a: %v", err)
	}
	ea, err := eng.Get("a")
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	if ea.Epoch == 0 {
		t.Fatalf("epoch must be stamped from the raft index, got 0")
	}

	if _, err := eng.Set("b", []byte("2")); err != nil {
		t.Fatalf("set b: %v", err)
	}
	eb, err := eng.Get("b")
	if err != nil {
		t.Fatalf("get b: %v", err)
	}
	if eb.Epoch <= ea.Epoch {
		t.Fatalf("epoch must advance with the raft index: a=%d b=%d", ea.Epoch, eb.Epoch)
	}
}

func TestRaftEngine_ScanAtIndex(t *testing.T) {
	eng, _ := newEngine(t)
	for _, k := range []string{"p:1", "p:2", "q:1"} {
		if _, err := eng.Set(k, []byte("v")); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}
	var seen int
	idx, err := eng.ScanAtIndex("p:", func(kvapi.Entry) bool { seen++; return true })
	if err != nil {
		t.Fatalf("scan-at-index: %v", err)
	}
	if seen != 2 {
		t.Fatalf("prefix scan visited %d, want 2", seen)
	}
	if idx == 0 {
		t.Fatalf("scan-at-index must return a non-zero commit index")
	}
}

func TestRaftFSM_SnapshotRestorePreservesEpoch(t *testing.T) {
	eng, fsm := newEngine(t)
	if _, err := eng.Set("k", []byte("v")); err != nil {
		t.Fatalf("set: %v", err)
	}
	want, _ := eng.Get("k")

	snap, err := fsm.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	sink := &memSink{}
	if err := snap.Persist(sink); err != nil {
		t.Fatalf("persist: %v", err)
	}

	fresh := NewRaftFSM(nil)
	if err := fresh.Restore(io.NopCloser(bytes.NewReader(sink.buf))); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, ok := fresh.get("k")
	if !ok {
		t.Fatalf("key missing after restore")
	}
	if got.Epoch != want.Epoch || got.Epoch == 0 {
		t.Fatalf("epoch not preserved: got %d want %d", got.Epoch, want.Epoch)
	}
}

// TestRaftFSM_RestoreLegacySnapshot proves a snapshot written by an older binary
// (no Epoch field) restores cleanly, with Epoch defaulting to 0.
func TestRaftFSM_RestoreLegacySnapshot(t *testing.T) {
	type legacyEntry struct {
		Key     string
		LeaseID string
		Value   []byte
		Version uint64
	}
	type legacyState struct {
		Entries []legacyEntry
		Leases  []snapLease
		Version uint64
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(legacyState{
		Entries: []legacyEntry{{Key: "old", Value: []byte("v"), Version: 3}},
		Version: 3,
	}); err != nil {
		t.Fatalf("encode legacy: %v", err)
	}

	fsm := NewRaftFSM(nil)
	if err := fsm.Restore(io.NopCloser(bytes.NewReader(buf.Bytes()))); err != nil {
		t.Fatalf("restore legacy: %v", err)
	}
	got, ok := fsm.get("old")
	if !ok {
		t.Fatalf("legacy key missing after restore")
	}
	if got.Epoch != 0 || got.Version != 3 {
		t.Fatalf("legacy entry mismatch: epoch=%d version=%d", got.Epoch, got.Version)
	}
}
