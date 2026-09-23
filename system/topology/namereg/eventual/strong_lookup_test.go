// SPDX-License-Identifier: MPL-2.0

package eventual_test

import (
	"context"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/topology/namereg/admission"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

func TestLookupDoesNotExposeConflictingDotUnderStrongReservation(t *testing.T) {
	ctx := context.Background()
	strong := pid.PID{Node: "node-B", Host: "host", UniqID: "strong"}
	stale := pid.PID{Node: "node-A", Host: "host", UniqID: "stale"}
	var reserved bool
	svc := eventual.NewService(eventual.Config{
		LocalNodeID: "node-B",
		Admission:   &admission.Coordinator{},
		StrongReservation: func(string) (pid.PID, bool) {
			return strong, reserved
		},
	})

	reserved = true
	if got, err := svc.Lookup(ctx, "claim"); err != nil || got.Found {
		t.Fatalf("reservation fabricated pending owner: %+v, err=%v", got, err)
	}
	frame, err := remoteDelta("claim", stale, "node-A", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	svc.OnFrame(frameOf(t, frame)) // delayed pre-Strong gossip after ACK
	if got, found := svc.State().Lookup("claim"); !found || !got.Equal(stale) {
		t.Fatalf("raw CRDT dot was unexpectedly discarded: %v, found=%v", got, found)
	}
	if got, err := svc.Lookup(ctx, "claim"); err != nil || got.Found {
		t.Fatalf("conflicting delayed dot escaped reservation: %+v, err=%v", got, err)
	}
	if got, found, err := svc.ConflictingLiveClaim("claim", strong); err != nil || !found || !got.Equal(stale) {
		t.Fatalf("reservation hid raw voting conflict: owner=%v found=%v err=%v", got, found, err)
	}

	reserved = false
	if got, err := svc.Lookup(ctx, "claim"); err != nil || !got.Found || !got.PID.Equal(stale) {
		t.Fatalf("released reservation did not restore EVENTUAL view: %+v, err=%v", got, err)
	}
}

func TestLookupKeepsMatchingEventualClaimUnderStrongReservation(t *testing.T) {
	ctx := context.Background()
	owner := pid.PID{Node: "node-A", Host: "host", UniqID: "owner"}
	svc := eventual.NewService(eventual.Config{
		LocalNodeID: "node-A",
		Admission:   &admission.Coordinator{},
		StrongReservation: func(string) (pid.PID, bool) {
			return owner, true
		},
	})
	if _, err := svc.Register("claim", owner); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Lookup(ctx, "claim"); err != nil || !got.Found || !got.PID.Equal(owner) {
		t.Fatalf("matching EVENTUAL claim was hidden: %+v, err=%v", got, err)
	}
}
