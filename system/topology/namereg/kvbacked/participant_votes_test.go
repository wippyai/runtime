// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"errors"
	"strings"
	"testing"

	"github.com/wippyai/runtime/api/pid"
)

func TestParticipantVoteKeysAreBoundAndCollisionResistant(t *testing.T) {
	node := pid.NodeID("node:α")
	name := "svc:東京"
	bound := pendingHeader{RequiredIncarnations: map[pid.NodeID]string{node: "inc:一"}}
	ack := bound.ackKey(name, 17, node)
	reject := bound.rejectKey(name, 17, node)
	if ack == reject {
		t.Fatal("ack and reject keys collided")
	}
	if !strings.HasPrefix(ack, participantAckPrefix) || !strings.HasPrefix(reject, participantRejectPrefix) {
		t.Fatalf("bound key prefixes: ack=%q reject=%q", ack, reject)
	}
	if ack == ackKey(name, 17, node) || reject == rejectKey(name, 17, node) {
		t.Fatal("bound vote key fell back to node-only format")
	}

	// Raw colon-delimited components would collide for these two tuples. Each
	// component is encoded independently, so their bound keys remain distinct.
	first := pendingHeader{RequiredIncarnations: map[pid.NodeID]string{"node:a": "inc"}}
	second := pendingHeader{RequiredIncarnations: map[pid.NodeID]string{"node": "a:inc"}}
	if first.ackKey("name", 1, "node:a") == second.ackKey("name", 1, "node") {
		t.Fatal("component encoding allowed a colon collision")
	}
	if bound.ackKey(name, 17, node) == (pendingHeader{RequiredIncarnations: map[pid.NodeID]string{node: "inc:二"}}).ackKey(name, 17, node) {
		t.Fatal("different incarnation tokens collided")
	}
}

func TestParticipantVoteKeysForLegacyHeadersUseExistingFormat(t *testing.T) {
	hdr := pendingHeader{}
	name := "legacy:name"
	node := pid.NodeID("node:1")
	if got, want := hdr.ackKey(name, 4, node), ackKey(name, 4, node); got != want {
		t.Fatalf("legacy ack key=%q, want %q", got, want)
	}
	if got, want := hdr.rejectKey(name, 4, node), rejectKey(name, 4, node); got != want {
		t.Fatalf("legacy reject key=%q, want %q", got, want)
	}
}

func TestBoundHeaderNeverUsesLegacyVoteKeyWhenIncarnationMissing(t *testing.T) {
	node := pid.NodeID("node-1")
	hdr := pendingHeader{RequiredIncarnations: map[pid.NodeID]string{}}
	if got := hdr.ackKey("name", 1, node); got == ackKey("name", 1, node) {
		t.Fatal("bound header used legacy ack key")
	}
	if got := hdr.rejectKey("name", 1, node); got == rejectKey("name", 1, node) {
		t.Fatal("bound header used legacy reject key")
	}
	if err := validateRequiredIncarnations(hdr); err == nil {
		t.Fatal("malformed bound header unexpectedly validated")
	}
}

func TestValidateRequiredIncarnations(t *testing.T) {
	if err := validateRequiredIncarnations(pendingHeader{}); err != nil {
		t.Fatalf("nil incarnation map must preserve legacy records: %v", err)
	}
	valid := pendingHeader{
		RequiredNodes: []pid.NodeID{"node-1", "node-2"},
		RequiredIncarnations: map[pid.NodeID]string{
			"node-1": "inc-1",
			"node-2": "inc-2",
		},
	}
	if err := validateRequiredIncarnations(valid); err != nil {
		t.Fatalf("valid bound header rejected: %v", err)
	}

	tests := []struct {
		name string
		hdr  pendingHeader
	}{
		{"no required nodes", pendingHeader{RequiredIncarnations: map[pid.NodeID]string{"node-1": "inc-1"}}},
		{"missing map entry", pendingHeader{RequiredNodes: []pid.NodeID{"node-1", "node-2"}, RequiredIncarnations: map[pid.NodeID]string{"node-1": "inc-1"}}},
		{"extra map entry", pendingHeader{RequiredNodes: []pid.NodeID{"node-1"}, RequiredIncarnations: map[pid.NodeID]string{"node-1": "inc-1", "node-2": "inc-2"}}},
		{"duplicate node", pendingHeader{RequiredNodes: []pid.NodeID{"node-1", "node-1"}, RequiredIncarnations: map[pid.NodeID]string{"node-1": "inc-1"}}},
		{"empty node", pendingHeader{RequiredNodes: []pid.NodeID{""}, RequiredIncarnations: map[pid.NodeID]string{"": "inc-1"}}},
		{"empty incarnation", pendingHeader{RequiredNodes: []pid.NodeID{"node-1"}, RequiredIncarnations: map[pid.NodeID]string{"node-1": " "}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateRequiredIncarnations(tc.hdr); !errors.Is(err, errInvalidRequiredIncarnations) {
				t.Fatalf("error=%v, want invalid-incarnation error", err)
			}
		})
	}
}
