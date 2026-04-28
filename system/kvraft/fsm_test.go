// SPDX-License-Identifier: MPL-2.0

package kvraft

import (
	"errors"
	"testing"
	"time"

	hraft "github.com/hashicorp/raft"
	"github.com/wippyai/runtime/api/kv"
)

func applyCmd(t *testing.T, fsm *FSM, cmd *Command, index uint64) *Result {
	t.Helper()
	data, err := EncodeCommand(cmd)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp := fsm.Apply(&hraft.Log{Index: index, Data: data})
	r, ok := resp.(*Result)
	if !ok {
		t.Fatalf("unexpected apply response type: %T", resp)
	}
	return r
}

func TestFSM_PutGet(t *testing.T) {
	fsm := NewFSM()
	r := applyCmd(t, fsm, &Command{Type: CmdPut, Space: "s", Key: "k", Value: []byte("v")}, 1)
	if r.Err != nil {
		t.Fatalf("put err: %v", r.Err)
	}
	if r.Version != 1 {
		t.Errorf("version=%d want 1", r.Version)
	}
	value, version, _, ok := fsm.State().get(shardKey{Space: "s", Key: "k"}, time.Now().UnixMilli())
	if !ok || string(value) != "v" || version != 1 {
		t.Errorf("get mismatch: ok=%v val=%q version=%d", ok, value, version)
	}
}

func TestFSM_Delete(t *testing.T) {
	fsm := NewFSM()
	applyCmd(t, fsm, &Command{Type: CmdPut, Space: "s", Key: "k", Value: []byte("v")}, 1)
	applyCmd(t, fsm, &Command{Type: CmdDelete, Space: "s", Key: "k"}, 2)
	_, _, _, ok := fsm.State().get(shardKey{Space: "s", Key: "k"}, time.Now().UnixMilli()) //nolint:dogsled // existence test only
	if ok {
		t.Errorf("expected key absent after delete")
	}
}

func TestFSM_CAS_Success(t *testing.T) {
	fsm := NewFSM()
	applyCmd(t, fsm, &Command{Type: CmdPut, Space: "s", Key: "k", Value: []byte("v1")}, 1)
	r := applyCmd(t, fsm, &Command{
		Type: CmdCAS, Space: "s", Key: "k",
		ExpectValue: []byte("v1"), Value: []byte("v2"),
	}, 2)
	if r.Err != nil {
		t.Fatalf("CAS err: %v", r.Err)
	}
	value, _, _, _ := fsm.State().get(shardKey{Space: "s", Key: "k"}, time.Now().UnixMilli()) //nolint:dogsled // value-only test
	if string(value) != "v2" {
		t.Errorf("got %q want v2", value)
	}
}

func TestFSM_CAS_Mismatch(t *testing.T) {
	fsm := NewFSM()
	applyCmd(t, fsm, &Command{Type: CmdPut, Space: "s", Key: "k", Value: []byte("v1")}, 1)
	r := applyCmd(t, fsm, &Command{
		Type: CmdCAS, Space: "s", Key: "k",
		ExpectValue: []byte("WRONG"), Value: []byte("v2"),
	}, 2)
	if !errors.Is(r.Err, kv.ErrCASMismatch) {
		t.Errorf("err=%v want ErrCASMismatch", r.Err)
	}
}

func TestFSM_PutWithExpectAbsent(t *testing.T) {
	fsm := NewFSM()
	r := applyCmd(t, fsm, &Command{
		Type: CmdPut, Space: "s", Key: "k", Value: []byte("v"),
		ExpectAbsent: true,
	}, 1)
	if r.Err != nil {
		t.Fatalf("first put: %v", r.Err)
	}
	r = applyCmd(t, fsm, &Command{
		Type: CmdPut, Space: "s", Key: "k", Value: []byte("v2"),
		ExpectAbsent: true,
	}, 2)
	if !errors.Is(r.Err, kv.ErrKeyExists) {
		t.Errorf("err=%v want ErrKeyExists", r.Err)
	}
}

func TestFSM_PutWithExpectVersion(t *testing.T) {
	fsm := NewFSM()
	applyCmd(t, fsm, &Command{Type: CmdPut, Space: "s", Key: "k", Value: []byte("v1")}, 5)
	r := applyCmd(t, fsm, &Command{
		Type: CmdPut, Space: "s", Key: "k", Value: []byte("v2"),
		ExpectVersion: 5,
	}, 6)
	if r.Err != nil {
		t.Fatalf("matching version: %v", r.Err)
	}
	r = applyCmd(t, fsm, &Command{
		Type: CmdPut, Space: "s", Key: "k", Value: []byte("v3"),
		ExpectVersion: 999,
	}, 7)
	if !errors.Is(r.Err, kv.ErrCASMismatch) {
		t.Errorf("stale-version err=%v want ErrCASMismatch", r.Err)
	}
}

func TestFSM_TTLExpiration(t *testing.T) {
	fsm := NewFSM()
	pinned := int64(1_000_000)
	original := nowMs
	nowMs = func() int64 { return pinned }
	defer func() { nowMs = original }()

	expireAt := pinned + 1000
	applyCmd(t, fsm, &Command{Type: CmdPut, Space: "s", Key: "k", Value: []byte("v"), TTL: expireAt}, 1)

	// Before expiry
	_, _, _, ok := fsm.State().get(shardKey{Space: "s", Key: "k"}, pinned+500) //nolint:dogsled // existence test only
	if !ok {
		t.Errorf("expected live before expiry")
	}
	// At/after expiry
	_, _, _, ok = fsm.State().get(shardKey{Space: "s", Key: "k"}, expireAt+1) //nolint:dogsled // existence test only
	if ok {
		t.Errorf("expected expired")
	}
}

func TestFSM_ReapTTL(t *testing.T) {
	fsm := NewFSM()
	now := int64(1_000_000)
	applyCmd(t, fsm, &Command{Type: CmdPut, Space: "s", Key: "expired", Value: []byte("x"), TTL: now - 1000}, 1)
	applyCmd(t, fsm, &Command{Type: CmdPut, Space: "s", Key: "live", Value: []byte("v")}, 2)

	original := nowMs
	nowMs = func() int64 { return now }
	defer func() { nowMs = original }()

	r := applyCmd(t, fsm, &Command{Type: CmdReapTTL}, 3)
	if r.Removed != 1 {
		t.Errorf("removed=%d want 1", r.Removed)
	}
	if _, _, _, ok := fsm.State().get(shardKey{Space: "s", Key: "live"}, now); !ok {
		t.Errorf("live key reaped by accident")
	}
}

func TestFSM_SnapshotRestoreRoundTrip(t *testing.T) {
	original := NewFSM()
	for i := 0; i < 10; i++ {
		applyCmd(t, original, &Command{
			Type: CmdPut, Space: "s",
			Key:   "k" + string(rune('0'+i)),
			Value: []byte("v"),
		}, uint64(i+1))
	}

	snap, err := original.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	sink := &mockSnapshotSink{}
	if err := snap.Persist(sink); err != nil {
		t.Fatalf("persist: %v", err)
	}

	restored := NewFSM()
	if err := restored.Restore(&mockReader{data: sink.data}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.State().Len() != 10 {
		t.Errorf("len=%d want 10", restored.State().Len())
	}
	for i := 0; i < 10; i++ {
		_, _, _, ok := restored.State().get(shardKey{Space: "s", Key: "k" + string(rune('0'+i))}, time.Now().UnixMilli())
		if !ok {
			t.Errorf("key k%d missing after restore", i)
		}
	}
}

func TestFSM_ApplyCallback(t *testing.T) {
	fsm := NewFSM()
	type callback struct {
		key     string
		version uint64
		op      kv.Op
	}
	var got []callback
	fsm.SetApplyCallback(func(op kv.Op, _, key string, _ []byte, version uint64) {
		got = append(got, callback{key: key, version: version, op: op})
	})

	applyCmd(t, fsm, &Command{Type: CmdPut, Space: "s", Key: "a", Value: []byte("v")}, 1)
	applyCmd(t, fsm, &Command{Type: CmdDelete, Space: "s", Key: "a"}, 2)
	applyCmd(t, fsm, &Command{Type: CmdPut, Space: "s", Key: "b", Value: []byte("v")}, 3)
	applyCmd(t, fsm, &Command{Type: CmdDelete, Space: "s", Key: "missing"}, 4) // no callback

	if len(got) != 3 {
		t.Errorf("callbacks=%d want 3: %+v", len(got), got)
	}
}

// --- helpers ---

type mockSnapshotSink struct {
	data []byte
}

func (m *mockSnapshotSink) Write(p []byte) (int, error) {
	m.data = append(m.data, p...)
	return len(p), nil
}
func (m *mockSnapshotSink) Close() error  { return nil }
func (m *mockSnapshotSink) ID() string    { return "test" }
func (m *mockSnapshotSink) Cancel() error { return nil }

type mockReader struct {
	data []byte
	pos  int
}

func (m *mockReader) Read(p []byte) (int, error) {
	if m.pos >= len(m.data) {
		return 0, errReadEOF
	}
	n := copy(p, m.data[m.pos:])
	m.pos += n
	return n, nil
}
func (m *mockReader) Close() error { return nil }

var errReadEOF = errors.New("EOF")
