// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"bytes"
	"io"
	"testing"

	hraft "github.com/hashicorp/raft"
)

// TestRaftFSM_RestoreEmptyResetsState proves the kv FSM's reset-to-empty
// contract: an empty Restore stream (fed by the multiplex router when a snapshot
// carries no kv section) clears existing state instead of erroring.
func TestRaftFSM_RestoreEmptyResetsState(t *testing.T) {
	fsm := NewRaftFSM(nil)
	fsm.Apply(&hraft.Log{Index: 1, Data: encodeCommand(command{Op: opSet, Key: "k", Value: []byte("v")})})
	fsm.Apply(&hraft.Log{Index: 2, Data: encodeCommand(command{Op: opLeaseGrant, LeaseID: "L", TTLms: 1000, ExpiresAtMs: 1})})
	if _, ok := fsm.get("k"); !ok {
		t.Fatal("precondition: key should exist before reset")
	}

	if err := fsm.Restore(io.NopCloser(bytes.NewReader(nil))); err != nil {
		t.Fatalf("restore empty: %v", err)
	}

	if _, ok := fsm.get("k"); ok {
		t.Fatalf("empty restore did not clear entries")
	}
	if len(fsm.leaseSnapshot()) != 0 {
		t.Fatalf("empty restore did not clear leases")
	}
}
