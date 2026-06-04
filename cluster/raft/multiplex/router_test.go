// SPDX-License-Identifier: MPL-2.0

package multiplex

import (
	"bytes"
	"io"
	"strings"
	"testing"

	hraft "github.com/hashicorp/raft"
)

// fakeFSM records applied command payloads and (de)serializes them newline-joined.
type fakeFSM struct {
	applied []string
}

func (f *fakeFSM) Apply(l *hraft.Log) any {
	f.applied = append(f.applied, string(l.Data))
	return len(f.applied)
}

func (f *fakeFSM) Snapshot() (hraft.FSMSnapshot, error) {
	return &fakeSnap{data: strings.Join(f.applied, "\n")}, nil
}

func (f *fakeFSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	f.applied = nil
	if len(b) > 0 {
		f.applied = strings.Split(string(b), "\n")
	}
	return nil
}

type fakeSnap struct{ data string }

func (s *fakeSnap) Persist(sink hraft.SnapshotSink) error {
	if _, err := sink.Write([]byte(s.data)); err != nil {
		_ = sink.Cancel()
		return err
	}
	return sink.Close()
}

func (s *fakeSnap) Release() {}

// memSink is an in-memory hraft.SnapshotSink.
type memSink struct {
	bytes.Buffer
}

func (m *memSink) Close() error  { return nil }
func (m *memSink) ID() string    { return "test" }
func (m *memSink) Cancel() error { return nil }

func persist(t *testing.T, snap hraft.FSMSnapshot) []byte {
	t.Helper()
	var sink memSink
	if err := snap.Persist(&sink); err != nil {
		t.Fatalf("persist: %v", err)
	}
	snap.Release()
	return sink.Bytes()
}

func kvCmd(s string) []byte { return append([]byte{KVDomain}, s...) }

func TestRouter_Apply(t *testing.T) {
	primary, kv := &fakeFSM{}, &fakeFSM{}
	r := New(primary, kv)

	r.Apply(&hraft.Log{Data: []byte("g1")})
	r.Apply(&hraft.Log{Data: kvCmd("k1")})
	r.Apply(&hraft.Log{Data: []byte("g2")})

	if got := strings.Join(primary.applied, ","); got != "g1,g2" {
		t.Fatalf("primary applied = %q, want g1,g2", got)
	}
	if got := strings.Join(kv.applied, ","); got != "k1" {
		t.Fatalf("kv applied = %q, want k1 (domain byte must be stripped)", got)
	}
}

func TestRouter_SnapshotRestoreRoundTrip(t *testing.T) {
	primary, kv := &fakeFSM{}, &fakeFSM{}
	r := New(primary, kv)
	r.Apply(&hraft.Log{Data: []byte("g1")})
	r.Apply(&hraft.Log{Data: []byte("g2")})
	r.Apply(&hraft.Log{Data: kvCmd("k1")})

	snap, err := r.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	data := persist(t, snap)

	p2, k2 := &fakeFSM{}, &fakeFSM{}
	r2 := New(p2, k2)
	if err := r2.Restore(io.NopCloser(bytes.NewReader(data))); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := strings.Join(p2.applied, ","); got != "g1,g2" {
		t.Fatalf("restored primary = %q", got)
	}
	if got := strings.Join(k2.applied, ","); got != "k1" {
		t.Fatalf("restored kv = %q", got)
	}
}

func TestRouter_LegacySnapshotFallback(t *testing.T) {
	// A bare primary snapshot written before the router existed.
	bare := &fakeFSM{applied: []string{"old1", "old2"}}
	bareSnap, _ := bare.Snapshot()
	data := persist(t, bareSnap)
	if bytes.HasPrefix(data, snapshotMagic[:]) {
		t.Fatalf("legacy fixture unexpectedly starts with magic")
	}

	p, k := &fakeFSM{}, &fakeFSM{}
	r := New(p, k)
	if err := r.Restore(io.NopCloser(bytes.NewReader(data))); err != nil {
		t.Fatalf("restore legacy: %v", err)
	}
	if got := strings.Join(p.applied, ","); got != "old1,old2" {
		t.Fatalf("legacy restore to primary = %q, want old1,old2", got)
	}
	if len(k.applied) != 0 {
		t.Fatalf("kv should be empty after legacy restore, got %v", k.applied)
	}
}

func TestRouter_NilKVTransparent(t *testing.T) {
	primary := &fakeFSM{}
	r := New(primary, nil)
	r.Apply(&hraft.Log{Data: []byte("g1")})
	r.Apply(&hraft.Log{Data: kvCmd("k1")}) // no kv FSM: goes to primary verbatim

	if len(primary.applied) != 2 {
		t.Fatalf("primary should have both commands, got %v", primary.applied)
	}

	snap, err := r.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	data := persist(t, snap)

	// Restore into a router that DOES have a kv FSM: kv stays empty (no section).
	p2, k2 := &fakeFSM{}, &fakeFSM{}
	if err := New(p2, k2).Restore(io.NopCloser(bytes.NewReader(data))); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(p2.applied) != 2 || len(k2.applied) != 0 {
		t.Fatalf("nil-kv snapshot restored wrong: p=%v k=%v", p2.applied, k2.applied)
	}
}

// TestRouter_KVDomainOutsideMsgpackHeaderRange guards the choice of KVDomain:
// the global registry encodes commands as msgpack maps, whose first byte is a
// fixmap (0x80-0x8f) or map16/map32 (0xde/0xdf). KVDomain must avoid those so
// an untagged command can never be mistaken for kv and vice versa.
func TestRouter_KVDomainOutsideMsgpackHeaderRange(t *testing.T) {
	if KVDomain >= 0x80 && KVDomain <= 0x8f {
		t.Fatalf("KVDomain 0x%02x collides with msgpack fixmap range", KVDomain)
	}
	if KVDomain == 0xde || KVDomain == 0xdf {
		t.Fatalf("KVDomain 0x%02x collides with msgpack map16/map32", KVDomain)
	}
}
