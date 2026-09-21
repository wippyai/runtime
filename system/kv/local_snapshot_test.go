// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestLocalSnapshotPreservesAtomicKeyTransfer(t *testing.T) {
	s := NewService("snapshot", nil)
	if _, err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())
	if _, err := s.Set("pending", []byte("owner")); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 1000 {
			from, to := "pending", "active"
			if i%2 != 0 {
				from, to = to, from
			}
			if ok, err := s.Txn([]kvapi.TxnOp{
				{Kind: kvapi.TxnDelete, Key: from},
				{Kind: kvapi.TxnPut, Key: to, Value: []byte(fmt.Sprint(i))},
			}); !ok || err != nil {
				t.Errorf("transfer: committed=%v err=%v", ok, err)
				return
			}
		}
	})
	defer wg.Wait()

	for range 1000 {
		entries, revision, err := s.ReadLocalSnapshot([]string{"pending", "active", "absent"})
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || revision == 0 {
			t.Fatalf("torn transfer: revision=%d entries=%v", revision, entries)
		}
	}
}

func TestLocalSnapshotIndexAndCopy(t *testing.T) {
	f := NewRaftFSM()
	f.state.applyIndex = 42
	f.state.set("key", []byte("value"), "")
	f.snap.Store(f.state.snapshot())
	e := &RaftEngine{fsm: f}

	entries, index, err := e.ReadLocalSnapshot([]string{"key", "key", "missing"})
	if err != nil || index != 42 || len(entries) != 1 {
		t.Fatalf("snapshot index=%d entries=%v err=%v", index, entries, err)
	}
	entries["key"].Value[0] = 'X'
	next, _, err := e.ReadLocalSnapshot([]string{"key"})
	if err != nil || string(next["key"].Value) != "value" {
		t.Fatal("snapshot exposes mutable replica bytes")
	}
}

func TestLocalSnapshotClosedEngineFails(t *testing.T) {
	s := NewService("snapshot", nil)
	if _, _, err := s.ReadLocalSnapshot([]string{"key"}); !errors.Is(err, kvapi.ErrKVClosed) {
		t.Fatalf("closed in-memory engine error=%v", err)
	}
	f := NewRaftFSM()
	if _, _, err := (&RaftEngine{fsm: f}).ReadLocalSnapshot([]string{"key"}); err != nil {
		t.Fatalf("initialized raft FSM should publish an empty snapshot: %v", err)
	}
	if _, _, err := (&RaftEngine{}).ReadLocalSnapshot([]string{"key"}); !errors.Is(err, kvapi.ErrKVClosed) {
		t.Fatalf("nil raft FSM error=%v", err)
	}
}

func TestLocalSnapshotDeletionRevisionAndStop(t *testing.T) {
	s := NewService("snapshot-deletion", nil)
	if _, err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	version, err := s.Set("key", []byte("value"))
	if err != nil {
		t.Fatal(err)
	}
	before, revision, err := s.ReadLocalSnapshot([]string{"key"})
	if err != nil || len(before) != 1 {
		t.Fatalf("initial snapshot: entries=%v err=%v", before, err)
	}
	if err := s.Delete("key"); err != nil {
		t.Fatal(err)
	}
	after, deletedRevision, err := s.ReadLocalSnapshot([]string{"key"})
	if err != nil || len(after) != 0 || deletedRevision <= revision {
		t.Fatalf("delete snapshot: entries=%v revision=%d previous=%d err=%v", after, deletedRevision, revision, err)
	}
	if string(before["key"].Value) != "value" {
		t.Fatal("deletion changed the caller-owned snapshot")
	}
	next, err := s.Set("key", []byte("replacement"))
	if err != nil || next != version+1 {
		t.Fatalf("entry version changed by deletion: version=%d want=%d err=%v", next, version+1, err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadLocalSnapshot(nil); !errors.Is(err, kvapi.ErrKVClosed) {
		t.Fatalf("stopped snapshot read: %v", err)
	}
}
