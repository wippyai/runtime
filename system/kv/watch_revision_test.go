// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"testing"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestWatchTransactionSharesPublicationRevision(t *testing.T) {
	s := NewService("watch-revision", nil)
	if _, err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	w, err := s.Watch(t.Context(), "key")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	committed, err := s.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnPut, Key: "key", Value: []byte("first")},
		{Kind: kvapi.TxnPut, Key: "key", Value: []byte("second")},
		{Kind: kvapi.TxnDelete, Key: "key"},
	})
	if err != nil || !committed {
		t.Fatalf("transaction: committed=%v err=%v", committed, err)
	}
	entries, revision, err := s.ReadLocalSnapshot([]string{"key"})
	if err != nil || len(entries) != 0 || revision == 0 {
		t.Fatalf("snapshot: entries=%v revision=%d err=%v", entries, revision, err)
	}
	first, second, deleted := nextPublished(t, w), nextPublished(t, w), nextPublished(t, w)
	for _, ev := range []kvapi.WatchEvent{first, second, deleted} {
		if ev.Revision != revision {
			t.Fatalf("event revision=%d, snapshot=%d", ev.Revision, revision)
		}
	}
	if first.Current == nil || string(first.Current.Value) != "first" ||
		second.Current == nil || string(second.Current.Value) != "second" ||
		deleted.Type != kvapi.WatchDelete || deleted.Previous == nil || string(deleted.Previous.Value) != "second" {
		t.Fatalf("lost intermediate operations: first=%+v second=%+v deleted=%+v", first, second, deleted)
	}
	if err := s.Delete("key"); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("missing delete: %v", err)
	}
	_, unchanged, err := s.ReadLocalSnapshot([]string{"key"})
	if err != nil || unchanged != revision {
		t.Fatalf("missing delete changed publication: before=%d after=%d err=%v", revision, unchanged, err)
	}
	version, err := s.Set("key", []byte("next"))
	if err != nil || version != 3 {
		t.Fatalf("delete renumbered entries: version=%d err=%v", version, err)
	}
	next := nextPublished(t, w)
	_, latest, err := s.ReadLocalSnapshot([]string{"key"})
	if err != nil || latest <= revision || next.Revision != latest {
		t.Fatalf("new publication: event=%d snapshot=%d previous=%d err=%v", next.Revision, latest, revision, err)
	}
}

func TestLocalSnapshotRevisionAdvancesOnDeleteOnlyPublication(t *testing.T) {
	s := NewService("snapshot", nil)
	if _, err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())
	if _, err := s.Set("key", []byte("old")); err != nil {
		t.Fatal(err)
	}
	_, before, err := s.ReadLocalSnapshot([]string{"key"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.Watch(t.Context(), "key")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := s.Delete("key"); err != nil {
		t.Fatal(err)
	}
	entries, after, err := s.ReadLocalSnapshot([]string{"key"})
	if err != nil || len(entries) != 0 || after <= before {
		t.Fatalf("delete did not advance the snapshot: before=%d after=%d entries=%v err=%v", before, after, entries, err)
	}
	deadline, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	select {
	case ev := <-w.Events():
		if ev.Type != kvapi.WatchDelete || ev.Revision != after {
			t.Fatalf("delete event revision=%d type=%v, want %d", ev.Revision, ev.Type, after)
		}
	case <-deadline.Done():
		t.Fatal("delete event unavailable")
	}
}
