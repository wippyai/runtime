// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"fmt"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"sync"
	"testing"
)

func TestLocalSnapshotPreservesAtomicKeyTransfer(t *testing.T) {
	s := NewService("snapshot", nil, nil)
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
			if ok, err := s.Txn([]kvapi.TxnOp{{Kind: kvapi.TxnDelete, Key: from}, {Kind: kvapi.TxnPut, Key: to, Value: []byte(fmt.Sprint(i))}}); !ok || err != nil {
				t.Errorf("transfer: %v %v", ok, err)
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
	f := NewRaftFSM(nil)
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
