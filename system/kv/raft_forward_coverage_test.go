// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"testing"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// startForwardCluster wires n engines on a shared in-process router, the first
// being the leader. leaderOf overrides which node each engine resolves as the
// leader, so a chain (client -> member -> leader) can be modeled.
func startForwardCluster(t *testing.T, leaderOf map[string]string) map[string]*RaftEngine {
	t.Helper()
	router := &routerTo{}
	engines := map[string]*RaftEngine{}
	for node, lead := range leaderOf {
		fsm := NewRaftFSM(nil)
		engines[node] = NewRaftEngine(
			&fakeRaft{fsm: fsm, leader: node == lead, leaderID: lead}, fsm, nil, node, router, nil)
	}
	router.engines = engines
	for _, e := range engines {
		if err := e.Start(context.Background()); err != nil {
			t.Fatalf("start %s: %v", e.localNode, err)
		}
	}
	t.Cleanup(func() {
		for _, e := range engines {
			_ = e.Stop()
		}
	})
	return engines
}

// TestRaftEngine_ForwardedReadViaLeader covers the forwarded-read path: a
// follower's GetViaLeader resolves against the leader's applied state.
func TestRaftEngine_ForwardedReadViaLeader(t *testing.T) {
	eng := startForwardCluster(t, map[string]string{"A": "A", "B": "A"})
	a, b := eng["A"], eng["B"]

	if _, err := a.Set("k", []byte("v")); err != nil {
		t.Fatalf("leader set: %v", err)
	}
	got, err := b.GetViaLeader("k")
	if err != nil || string(got.Value) != "v" {
		t.Fatalf("forwarded read = %+v err=%v, want v", got, err)
	}
	if _, err := b.GetViaLeader("missing"); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("forwarded read of missing key = %v, want ErrKeyNotFound", err)
	}
}

// TestRaftEngine_ReForwardThroughMember covers hop-bounded re-forwarding: node C
// resolves member B as leader, and B re-forwards both the write and the read to
// the real leader A.
func TestRaftEngine_ReForwardThroughMember(t *testing.T) {
	eng := startForwardCluster(t, map[string]string{"A": "A", "B": "A", "C": "B"})
	a, c := eng["A"], eng["C"]

	v, err := c.Set("k", []byte("v"))
	if err != nil {
		t.Fatalf("re-forwarded set: %v", err)
	}
	if v == 0 {
		t.Fatalf("re-forwarded set returned version 0")
	}
	if e, ok := a.fsm.get("k"); !ok || string(e.Value) != "v" {
		t.Fatalf("leader FSM missing re-forwarded write: %+v ok=%v", e, ok)
	}

	got, err := c.GetViaLeader("k")
	if err != nil || string(got.Value) != "v" {
		t.Fatalf("re-forwarded read = %+v err=%v, want v", got, err)
	}
}

// TestRaftEngine_ForwardedConditionalWrites covers forwarded conditional ops and
// the version-mismatch sentinel surviving the forward wire codec.
func TestRaftEngine_ForwardedConditionalWrites(t *testing.T) {
	eng := startForwardCluster(t, map[string]string{"A": "A", "B": "A"})
	b := eng["B"]

	v, stored, err := b.SetIfAbsent("k", []byte("v"))
	if err != nil || !stored || v == 0 {
		t.Fatalf("forwarded setIfAbsent: v=%d stored=%v err=%v", v, stored, err)
	}
	if _, stored, _ := b.SetIfAbsent("k", []byte("x")); stored {
		t.Fatalf("forwarded setIfAbsent on existing key should not store")
	}
	if _, ok, _ := b.CompareAndSwap("k", 999, []byte("x")); ok {
		t.Fatalf("forwarded CAS with wrong version should fail")
	}
	cur, err := b.GetViaLeader("k")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if _, ok, err := b.CompareAndSwap("k", cur.Version, []byte("v2")); err != nil || !ok {
		t.Fatalf("forwarded CAS with right version: ok=%v err=%v", ok, err)
	}
}

// TestDecodeCommand_MissingExpiresAtIsZero covers the tolerant decode of the
// trailing absolute-deadline field: a command encoded without it (a shorter
// buffer) decodes with ExpiresAtMs == 0 rather than erroring, so the field is
// append-only and backward tolerant.
func TestDecodeCommand_MissingExpiresAtIsZero(t *testing.T) {
	full := encodeCommand(command{Op: opLeaseGrant, LeaseID: "L", TTLms: 5000, ExpiresAtMs: 123})
	withoutExpiry := full[:len(full)-8]

	got, err := decodeCommand(withoutExpiry)
	if err != nil {
		t.Fatalf("decode without trailing expiresAt: %v", err)
	}
	if got.TTLms != 5000 || got.LeaseID != "L" || got.ExpiresAtMs != 0 {
		t.Fatalf("tolerant decode = %+v, want TTLms=5000 LeaseID=L ExpiresAtMs=0", got)
	}
}
