// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"errors"
	"testing"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestRaftWatchPreservesCommittedIntermediateOperations(t *testing.T) {
	eng, _ := newEngine(t)
	w, err := eng.Watch(t.Context(), "_sys:registry:result:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	key := "_sys:registry:result:claim"
	committed, err := eng.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: key, Value: []byte("terminal result")},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: key},
	})
	if err != nil || !committed {
		t.Fatalf("transaction: committed=%v err=%v", committed, err)
	}
	if _, err := eng.Get(key); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("transient key remained in published snapshot: %v", err)
	}
	_, revision, err := eng.ReadLocalSnapshot([]string{key})
	if err != nil {
		t.Fatal(err)
	}
	put, deleted := nextPublished(t, w), nextPublished(t, w)
	if put.Type != kvapi.WatchPut || put.Current == nil || string(put.Current.Value) != "terminal result" ||
		deleted.Type != kvapi.WatchDelete || deleted.Previous == nil || string(deleted.Previous.Value) != "terminal result" ||
		put.Revision != revision || put.Index == 0 || put.Index != deleted.Index || put.Revision != put.Index || deleted.Revision != put.Revision {
		t.Fatalf("lost ordered operations at one committed index: put=%+v delete=%+v", put, deleted)
	}
}
