// SPDX-License-Identifier: MPL-2.0

package multiplex

import (
	"bytes"
	"io"
	"strings"
	"testing"

	hraft "github.com/hashicorp/raft"
)

// TestRouter_ApplyEmptyDataGoesToPrimary pins the len(log.Data) > 0 guard: an
// empty entry (e.g. a raft no-op) must route to primary, never index log.Data[0].
func TestRouter_ApplyEmptyDataGoesToPrimary(t *testing.T) {
	primary, kv := &fakeFSM{}, &fakeFSM{}
	r := New(primary, kv)

	r.Apply(&hraft.Log{Data: nil})
	r.Apply(&hraft.Log{Data: []byte{}})

	if len(primary.applied) != 2 {
		t.Fatalf("empty entries should route to primary, got %v", primary.applied)
	}
	if len(kv.applied) != 0 {
		t.Fatalf("empty entries must not reach kv, got %v", kv.applied)
	}
}

// TestRouter_RestoreShortStreamIsLegacy pins the magic-read error guard: a stream
// shorter than the 4-byte magic (including empty) is a legacy bare-primary
// snapshot, not a read error, so it must restore to primary without erroring.
func TestRouter_RestoreShortStreamIsLegacy(t *testing.T) {
	for name, data := range map[string][]byte{"empty": {}, "two-bytes": {'h', 'i'}} {
		p, k := &fakeFSM{}, &fakeFSM{applied: []string{"stale"}}
		if err := New(p, k).Restore(io.NopCloser(bytes.NewReader(data))); err != nil {
			t.Fatalf("%s: restore short stream errored: %v", name, err)
		}
		if got := strings.Join(p.applied, ""); got != string(data) {
			t.Fatalf("%s: primary = %q, want %q", name, got, string(data))
		}
		if len(k.applied) != 0 {
			t.Fatalf("%s: kv not reset on legacy restore: %v", name, k.applied)
		}
	}
}

// cancelTrackingSink records whether Cancel was called, so a test can prove a
// successful Persist commits the sink instead of canceling it.
type cancelTrackingSink struct {
	bytes.Buffer
	cancelled bool
}

func (s *cancelTrackingSink) Close() error  { return nil }
func (s *cancelTrackingSink) ID() string    { return "test" }
func (s *cancelTrackingSink) Cancel() error { s.cancelled = true; return nil }

// TestRouter_PersistDoesNotCancelOnSuccess pins the kv-section error check in
// Persist: a successful kv write must not be treated as a failure and cancel the
// snapshot.
func TestRouter_PersistDoesNotCancelOnSuccess(t *testing.T) {
	r := New(&fakeFSM{applied: []string{"g1"}}, &fakeFSM{applied: []string{"k1"}})
	snap, err := r.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	var sink cancelTrackingSink
	if err := snap.Persist(&sink); err != nil {
		t.Fatalf("persist: %v", err)
	}
	snap.Release()
	if sink.cancelled {
		t.Fatalf("Persist cancelled the sink despite a successful kv-section write")
	}
}
