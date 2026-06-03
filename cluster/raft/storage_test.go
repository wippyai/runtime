// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"io"
	"path/filepath"
	"testing"

	hraft "github.com/hashicorp/raft"
)

func TestOpenStores_DisklessByDefault(t *testing.T) {
	ls, ss, snaps, closer, err := openStores("", 3, io.Discard)
	if err != nil {
		t.Fatalf("openStores diskless: %v", err)
	}
	defer func() { _ = closer() }()
	if ls == nil || ss == nil || snaps == nil {
		t.Fatalf("nil store(s): ls=%v ss=%v snaps=%v", ls, ss, snaps)
	}
	if _, ok := ls.(*hraft.InmemStore); !ok {
		t.Fatalf("diskless log store is %T, want *hraft.InmemStore", ls)
	}
}

func TestOpenStores_DurableRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "raft") // nested: MkdirAll must create it

	ls, ss, snaps, closer, err := openStores(dir, 2, io.Discard)
	if err != nil {
		t.Fatalf("openStores durable: %v", err)
	}
	if _, ok := ls.(*hraft.InmemStore); ok {
		t.Fatalf("durable log store must not be in-memory")
	}
	if snaps == nil {
		t.Fatalf("nil snapshot store")
	}

	if err := ls.StoreLogs([]*hraft.Log{{Index: 1, Term: 1, Data: []byte("hello")}}); err != nil {
		t.Fatalf("store log: %v", err)
	}
	if err := ss.SetUint64([]byte("term"), 7); err != nil {
		t.Fatalf("set stable: %v", err)
	}
	if err := closer(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen the same dir; durable state must survive.
	ls2, ss2, _, closer2, err := openStores(dir, 2, io.Discard)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = closer2() }()

	var l hraft.Log
	if err := ls2.GetLog(1, &l); err != nil {
		t.Fatalf("get log after reopen: %v", err)
	}
	if string(l.Data) != "hello" {
		t.Fatalf("log data after reopen = %q, want hello", l.Data)
	}
	v, err := ss2.GetUint64([]byte("term"))
	if err != nil {
		t.Fatalf("get stable after reopen: %v", err)
	}
	if v != 7 {
		t.Fatalf("stable term after reopen = %d, want 7", v)
	}
}
