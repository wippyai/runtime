// SPDX-License-Identifier: MPL-2.0

package multiplex

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestRouter_FramedRestoreResetsStaleKV is the H1 regression: a framed snapshot
// produced by a node with kv disabled carries only the primary section.
// Restoring it onto a node whose kv FSM already has state must clear that state,
// not leave it diverged from the cluster.
func TestRouter_FramedRestoreResetsStaleKV(t *testing.T) {
	primaryOnly := New(&fakeFSM{applied: []string{"g1"}}, nil)
	snap, err := primaryOnly.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	data := persist(t, snap)

	p, k := &fakeFSM{}, &fakeFSM{applied: []string{"stale1", "stale2"}}
	if err := New(p, k).Restore(io.NopCloser(bytes.NewReader(data))); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := strings.Join(p.applied, ","); got != "g1" {
		t.Fatalf("primary = %q, want g1", got)
	}
	if len(k.applied) != 0 {
		t.Fatalf("stale kv state not cleared on restore over existing state: %v", k.applied)
	}
}

// TestRouter_LegacyRestoreResetsStaleKV is the same invariant for a legacy
// bare-primary snapshot restored over a live kv FSM.
func TestRouter_LegacyRestoreResetsStaleKV(t *testing.T) {
	bare := &fakeFSM{applied: []string{"old1"}}
	bareSnap, _ := bare.Snapshot()
	data := persist(t, bareSnap)

	p, k := &fakeFSM{}, &fakeFSM{applied: []string{"stale"}}
	if err := New(p, k).Restore(io.NopCloser(bytes.NewReader(data))); err != nil {
		t.Fatalf("restore legacy: %v", err)
	}
	if got := strings.Join(p.applied, ","); got != "old1" {
		t.Fatalf("primary = %q, want old1", got)
	}
	if len(k.applied) != 0 {
		t.Fatalf("legacy restore left stale kv state: %v", k.applied)
	}
}
