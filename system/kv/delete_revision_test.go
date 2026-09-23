// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"io"
	"testing"

	hraft "github.com/hashicorp/raft"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestDeleteAdvancesPublishedRevision(t *testing.T) {
	s := NewService("revision", nil)
	if _, err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())

	first, err := s.Set("pending", []byte("owner"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.ScanAtIndex("pending", func(kvapi.Entry) bool { return true })
	if err != nil || before != first {
		t.Fatalf("initial snapshot revision=%d version=%d err=%v", before, first, err)
	}
	if err := s.Delete("pending"); err != nil {
		t.Fatal(err)
	}
	after, err := s.ScanAtIndex("pending", func(kvapi.Entry) bool {
		t.Fatal("deleted key survived in empty snapshot")
		return false
	})
	if err != nil || after <= before {
		t.Fatalf("empty snapshot revision=%d, before=%d err=%v", after, before, err)
	}
	if err := s.Delete("pending"); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("missing delete error=%v", err)
	}
	unchanged, err := s.ScanAtIndex("pending", func(kvapi.Entry) bool { return true })
	if err != nil || unchanged != after {
		t.Fatalf("failed delete advanced revision: before=%d after=%d err=%v", after, unchanged, err)
	}
	next, err := s.Set("pending", []byte("replacement"))
	if err != nil || next != first+1 {
		t.Fatalf("replacement entry version=%d, first=%d err=%v", next, first, err)
	}
	republished, err := s.ScanAtIndex("pending", func(kvapi.Entry) bool { return true })
	if err != nil || republished <= after {
		t.Fatalf("replacement publication revision=%d, deletion revision=%d err=%v", republished, after, err)
	}
}

func TestConditionalDeleteAdvancesOnlyCommittedPublication(t *testing.T) {
	s := NewService("conditional-revision", nil)
	if _, err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())

	version, err := s.Set("key", []byte("owner"))
	if err != nil {
		t.Fatal(err)
	}
	readRevision := func() uint64 {
		t.Helper()
		revision, err := s.ScanAtIndex("key", func(kvapi.Entry) bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		return revision
	}
	before := readRevision()
	if deleted, err := s.CompareAndDelete("key", version+1); err != nil || deleted || readRevision() != before {
		t.Fatalf("failed compare-and-delete: deleted=%v revision=%d err=%v", deleted, readRevision(), err)
	}
	if committed, err := s.Txn([]kvapi.TxnOp{{Kind: kvapi.TxnDelete, Cond: kvapi.CondVersion, Key: "key", Expect: version + 1}}); err != nil || committed || readRevision() != before {
		t.Fatalf("aborted txn: committed=%v revision=%d err=%v", committed, readRevision(), err)
	}
	if deleted, err := s.CompareAndDelete("key", version); err != nil || !deleted || readRevision() <= before {
		t.Fatalf("successful compare-and-delete: deleted=%v revision=%d err=%v", deleted, readRevision(), err)
	}
}

// Previously committed Raft commands may compare against the version of a
// write following a deletion. Replay must keep that version unchanged.
func TestDeleteDoesNotRenumberPreviouslyCommittedWrites(t *testing.T) {
	s := newState()
	_, first := s.set("a", []byte("old"), "")
	if first != 1 {
		t.Fatalf("first version=%d", first)
	}
	if s.del("a") == nil {
		t.Fatal("existing key was not deleted")
	}
	_, second := s.set("b", []byte("new"), "")
	if second != 2 {
		t.Fatalf("replayed write after delete version=%d, want 2", second)
	}
	next, ok := s.cas("b", second, []byte("updated"))
	if !ok || next != 3 {
		t.Fatalf("previously committed CAS: version=%d success=%v", next, ok)
	}
	if snapshot := s.snapshot(); snapshot.version <= next {
		t.Fatalf("publication revision=%d, entry version=%d", snapshot.version, next)
	}
}

func TestRaftReplayDeleteKeepsCommittedCASVersion(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(map[bool]string{false: "full-log", true: "snapshot-and-tail"}[restore], func(t *testing.T) {
			fsm := NewRaftFSM()
			apply := func(index uint64, c command) applyResult {
				t.Helper()
				return fsm.Apply(&hraft.Log{Index: index, Data: encodeCommand(c)}).(applyResult)
			}
			if res := apply(1, command{Op: opSet, Key: "a", Value: []byte("old")}); res.Version != 1 {
				t.Fatalf("first write version=%d", res.Version)
			}
			if restore {
				snapshot, err := fsm.Snapshot()
				if err != nil {
					t.Fatal(err)
				}
				var sink memSink
				if err := snapshot.Persist(&sink); err != nil {
					t.Fatal(err)
				}
				fsm = NewRaftFSM()
				if err := fsm.Restore(io.NopCloser(&sink)); err != nil {
					t.Fatal(err)
				}
			}
			if res := apply(2, command{Op: opDelete, Key: "a"}); res.Err != nil {
				t.Fatalf("delete: %v", res.Err)
			}
			if res := apply(3, command{Op: opSet, Key: "b", Value: []byte("new")}); res.Version != 2 {
				t.Fatalf("post-delete write version=%d, want legacy version 2", res.Version)
			}
			if res := apply(4, command{Op: opCAS, Key: "b", Expect: 2, Value: []byte("updated")}); !res.OK || res.Version != 3 {
				t.Fatalf("committed CAS after replay: %+v", res)
			}
		})
	}
}
