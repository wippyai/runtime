// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	hraft "github.com/hashicorp/raft"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/cluster/raft/multiplex"
	"github.com/wippyai/runtime/system/eventbus"
)

func TestEncodeDecodeCommand(t *testing.T) {
	cases := []command{
		{Op: opSet, Key: "k", Value: []byte("v")},
		{Op: opDelete, Key: "k"},
		{Op: opCAS, Key: "k", Value: []byte("v2"), Expect: 7},
		{Op: opSetIfAbsent, Key: "k", Value: []byte("v")},
		{Op: opSetWithLease, Key: "k", Value: []byte("v"), LeaseID: "l1"},
		{Op: opLeaseGrant, LeaseID: "l1", TTLms: 5000},
		{Op: opLeaseRevoke, LeaseID: "l1"},
		{Op: opSet, Key: "", Value: nil},
	}
	for _, c := range cases {
		got, err := decodeCommand(encodeCommand(c))
		if err != nil {
			t.Fatalf("decode %v: %v", c.Op, err)
		}
		if got.Op != c.Op || got.Key != c.Key || string(got.Value) != string(c.Value) ||
			got.Expect != c.Expect || got.LeaseID != c.LeaseID || got.TTLms != c.TTLms {
			t.Fatalf("round-trip mismatch: %+v != %+v", got, c)
		}
	}
}

// fakeRaft simulates a single-node leader: Apply strips the multiplex domain
// byte (as the router would) and applies straight to the FSM.
type fakeRaft struct {
	fsm    *RaftFSM
	leader bool
}

func (f *fakeRaft) IsLeader() bool { return f.leader }

func (f *fakeRaft) Apply(cmd []byte, _ time.Duration) (*raftapi.ApplyResponse, error) {
	if !f.leader {
		return nil, raftapi.ErrNotLeader
	}
	if len(cmd) == 0 || cmd[0] != multiplex.KVDomain {
		return nil, raftapi.ErrNotLeader
	}
	res := f.fsm.Apply(&hraft.Log{Data: cmd[1:]})
	return &raftapi.ApplyResponse{Response: res}, nil
}

func newEngine(t *testing.T) (*RaftEngine, *RaftFSM) {
	t.Helper()
	fsm := NewRaftFSM(eventbus.NewBus())
	eng := NewRaftEngine(&fakeRaft{fsm: fsm, leader: true}, fsm, eventbus.NewBus(), "node-1", nil)
	if err := eng.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop() })
	return eng, fsm
}

func TestRaftEngine_CRUD(t *testing.T) {
	eng, _ := newEngine(t)

	v, err := eng.Set("k", []byte("v"))
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := eng.Get("k")
	if err != nil || string(got.Value) != "v" || got.Version != v {
		t.Fatalf("get = %+v err=%v (want v/%d)", got, err, v)
	}

	v2, ok, err := eng.CompareAndSwap("k", v, []byte("v2"))
	if err != nil || !ok {
		t.Fatalf("cas: ok=%v err=%v", ok, err)
	}
	if _, ok, _ := eng.CompareAndSwap("k", 999, []byte("x")); ok {
		t.Fatalf("cas with wrong version should fail")
	}
	got, _ = eng.Get("k")
	if string(got.Value) != "v2" || got.Version != v2 {
		t.Fatalf("after cas = %+v", got)
	}

	if _, stored, _ := eng.SetIfAbsent("k", []byte("z")); stored {
		t.Fatalf("setIfAbsent on existing key should not store")
	}
	if _, stored, _ := eng.SetIfAbsent("fresh", []byte("z")); !stored {
		t.Fatalf("setIfAbsent on new key should store")
	}

	if err := eng.Delete("k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := eng.Get("k"); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("get after delete = %v, want ErrKeyNotFound", err)
	}
	if err := eng.Delete("k"); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("delete missing = %v, want ErrKeyNotFound", err)
	}
}

func TestRaftEngine_Scan(t *testing.T) {
	eng, _ := newEngine(t)
	_, _ = eng.Set("p/1", []byte("a"))
	_, _ = eng.Set("p/2", []byte("b"))
	_, _ = eng.Set("q/1", []byte("c"))

	n := 0
	if err := eng.Scan("p/", func(kvapi.Entry) bool { n++; return true }); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 2 {
		t.Fatalf("scan p/ found %d, want 2", n)
	}
}

func TestRaftEngine_FollowerWriteRejected(t *testing.T) {
	fsm := NewRaftFSM(nil)
	eng := NewRaftEngine(&fakeRaft{fsm: fsm, leader: false}, fsm, nil, "node-1", nil)
	if _, err := eng.Set("k", []byte("v")); !errors.Is(err, raftapi.ErrNotLeader) {
		t.Fatalf("follower set = %v, want ErrNotLeader", err)
	}
}

func TestRaftEngine_LeaseAutoExpiry(t *testing.T) {
	eng, _ := newEngine(t)

	lease, err := eng.GrantLease(context.Background(), 50*time.Millisecond)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err := eng.SetWithLease("lk", []byte("lv"), lease.ID()); err != nil {
		t.Fatalf("set with lease: %v", err)
	}
	if _, err := eng.Get("lk"); err != nil {
		t.Fatalf("get leased key: %v", err)
	}

	// Sweeper runs once per second; the 50ms lease must be revoked within ~2s.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := eng.Get("lk"); errors.Is(err, kvapi.ErrKeyNotFound) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("leased key was not auto-expired")
}

func TestRaftFSM_SnapshotRestore(t *testing.T) {
	fsm := NewRaftFSM(nil)
	apply := func(c command) { fsm.Apply(&hraft.Log{Data: encodeCommand(c)}) }
	apply(command{Op: opSet, Key: "a", Value: []byte("1")})
	apply(command{Op: opSet, Key: "b", Value: []byte("2")})
	apply(command{Op: opLeaseGrant, LeaseID: "L", TTLms: 10000})
	apply(command{Op: opSetWithLease, Key: "c", Value: []byte("3"), LeaseID: "L"})

	snap, err := fsm.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	var sink memSink
	if err := snap.Persist(&sink); err != nil {
		t.Fatalf("persist: %v", err)
	}

	restored := NewRaftFSM(nil)
	if err := restored.Restore(io.NopCloser(&sink)); err != nil {
		t.Fatalf("restore: %v", err)
	}
	for _, kv := range []struct{ k, v string }{{"a", "1"}, {"b", "2"}, {"c", "3"}} {
		e, ok := restored.get(kv.k)
		if !ok || string(e.Value) != kv.v {
			t.Fatalf("restored %q = %+v ok=%v, want %q", kv.k, e, ok, kv.v)
		}
	}
	if e, _ := restored.get("c"); e.LeaseID != "L" {
		t.Fatalf("restored lease binding lost: %+v", e)
	}
}

// memSink is an in-memory hraft.SnapshotSink for snapshot tests.
type memSink struct {
	buf []byte
	off int
}

func (m *memSink) Write(p []byte) (int, error) { m.buf = append(m.buf, p...); return len(p), nil }
func (m *memSink) Read(p []byte) (int, error) {
	if m.off >= len(m.buf) {
		return 0, io.EOF
	}
	n := copy(p, m.buf[m.off:])
	m.off += n
	return n, nil
}
func (m *memSink) Close() error  { return nil }
func (m *memSink) ID() string    { return "test" }
func (m *memSink) Cancel() error { return nil }
