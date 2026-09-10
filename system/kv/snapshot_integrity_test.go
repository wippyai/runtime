// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"fmt"
	"sync"
	"testing"

	hraft "github.com/hashicorp/raft"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestGetManyNeverTearsAtomicTransaction(t *testing.T) {
	e, _ := newEngine(t)
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for i := 0; i < 1000; i++ {
			value := []byte(fmt.Sprint(i))
			if ok, err := e.Txn([]kvapi.TxnOp{
				{Kind: kvapi.TxnPut, Key: "a", Value: value},
				{Kind: kvapi.TxnPut, Key: "b", Value: value},
			}); err != nil || !ok {
				t.Errorf("transaction: %v %v", ok, err)
				return
			}
		}
	}()
	defer writer.Wait()
	for i := 0; i < 1000; i++ {
		entries, err := e.fsm.snap.Load().getMany([]string{"a", "b", "missing"})
		if err != nil {
			t.Fatal(err)
		}
		a, aok := entries["a"]
		b, bok := entries["b"]
		if aok != bok || string(a.Value) != string(b.Value) {
			t.Fatalf("torn transaction: a=%+v b=%+v", a, b)
		}
		if _, found := entries["missing"]; found {
			t.Fatal("missing key included")
		}
		if len(a.Value) > 0 {
			a.Value[0] = 'X'
		}
	}
	writer.Wait()
	entries, err := e.fsm.snap.Load().getMany([]string{"a", "b"})
	if err != nil || string(entries["a"].Value) != "999" || string(entries["b"].Value) != "999" {
		t.Fatalf("returned values mutated replica: %v, %v", entries, err)
	}
}

func TestRaftReadValuesCannotMutateReplica(t *testing.T) {
	f := NewRaftFSM(nil)
	f.state.set("key", []byte("original"), "")
	f.snap.Store(f.state.snapshot())
	e, _ := f.get("key")
	e.Value[0] = 'X'
	e, _ = f.get("key")
	if string(e.Value) != "original" {
		t.Fatalf("Get exposed mutable replica bytes: %q", e.Value)
	}
	f.scan("key", func(e kvapi.Entry) bool { e.Value[0] = 'Y'; return true })
	e, _ = f.get("key")
	if string(e.Value) != "original" {
		t.Fatalf("Scan exposed mutable replica bytes: %q", e.Value)
	}
}

func TestServiceScanIndexMatchesCapturedValues(t *testing.T) {
	s := startTestService(t)
	version, err := s.Set("key", []byte("before"))
	if err != nil {
		t.Fatal(err)
	}
	var value string
	index, err := s.ScanAtIndex("key", func(e kvapi.Entry) bool {
		value = string(e.Value)
		if _, err := s.Set("key", []byte("after")); err != nil {
			t.Fatal(err)
		}
		return true
	})
	if err != nil || index != version || value != "before" {
		t.Fatalf("inconsistent scan: index=%d want=%d value=%q err=%v", index, version, value, err)
	}
}

func TestReadSnapshotRetainsEntriesAndAppliedIndex(t *testing.T) {
	eng, f := newEngine(t)
	_, err := eng.Set("a", []byte("before"))
	if err != nil {
		t.Fatal(err)
	}
	old := f.snap.Load()
	oldIndex := old.index
	_, err = eng.Set("a", []byte("after"))
	if err != nil {
		t.Fatal(err)
	}
	if got := old.get("a"); string(got.Value) != "before" || old.index != oldIndex {
		t.Fatal("old view changed")
	}
	// Model other commands committed in the shared Raft log, beyond this KV view.
	eng.raft.(*fakeRaft).index += 50
	var seen []string
	idx, err := eng.ScanAtIndex("", func(e kvapi.Entry) bool {
		seen = append(seen, string(e.Value))
		// Publication during a scan cannot replace the view being traversed.
		_, setErr := eng.Set("a", []byte("concurrent"))
		if setErr != nil {
			t.Fatal(setErr)
		}
		return true
	})
	if err != nil || idx != oldIndex+1 || len(seen) != 1 || seen[0] != "after" {
		t.Fatalf("torn scan: index=%d values=%v err=%v", idx, seen, err)
	}
}

func BenchmarkRaftApplyPopulated(b *testing.B) {
	for _, size := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			f := NewRaftFSM(nil)
			for i := 0; i < size; i++ {
				f.state.set(fmt.Sprintf("key/%d", i), []byte("value"), "")
			}
			f.snap.Store(f.state.snapshot())
			data := encodeCommand(command{Op: opSet, Key: "key/0", Value: []byte("updated")})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				f.Apply(&hraft.Log{Data: data, Index: uint64(i + 1)})
			}
		})
	}
}
