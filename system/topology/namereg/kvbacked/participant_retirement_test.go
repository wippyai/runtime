// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"testing"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestParticipantRetirementRejectsDelayedEnrollmentAndOldTransitions(t *testing.T) {
	p, _ := newParticipantTestInventory(t, 2)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(p.enroll(ctx, "node", "first"))
	_, staleReservation, err := p.readSnapshot()
	must(err)
	must(p.retire(ctx, "node", "first"))
	must(p.retire(ctx, "node", "first"))
	for _, inc := range []string{"first", "second"} {
		if err := p.enroll(ctx, "node", inc); !errors.Is(err, ErrParticipantRetired) {
			t.Fatalf("enroll %s: %v", inc, err)
		}
	}
	committed, err := p.engine.Txn([]kvapi.TxnOp{staleReservation, {Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: pendingKey("old"), Value: []byte("old")}})
	must(err)
	if committed {
		t.Fatal("pre-retirement inventory authorized a new reservation")
	}
	must(p.replace(ctx, "node", "first", "second"))
	must(p.replace(ctx, "node", "first", "second"))
	if err := p.enroll(ctx, "node", "first"); !errors.Is(err, ErrParticipantIncarnationConflict) {
		t.Fatalf("old enrollment: %v", err)
	}
	if err := p.retire(ctx, "node", "first"); !errors.Is(err, ErrParticipantIncarnationConflict) {
		t.Fatalf("old retirement: %v", err)
	}
	must(p.retire(ctx, "node", "second"))
	if err := p.replace(ctx, "node", "first", "second"); !errors.Is(err, ErrParticipantIncarnationConflict) {
		t.Fatalf("old replacement: %v", err)
	}
	active, _, err := p.readSnapshot()
	must(err)
	retired, _, err := p.retiredRecord()
	must(err)
	if len(active) != 0 || len(retired) != 1 || retired["node"] != "second" {
		t.Fatalf("active=%v retired=%v", active, retired)
	}
}

func TestParticipantRetirementKeepsBoundedIdentitySlots(t *testing.T) {
	p, _ := newParticipantTestInventory(t, 1)
	ctx := context.Background()
	for _, err := range []error{p.enroll(ctx, "node", "first"), p.retire(ctx, "node", "first")} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := p.enroll(ctx, "another", "first"); !errors.Is(err, ErrParticipantInventorySaturated) {
		t.Fatalf("retired slot disappeared: %v", err)
	}
	if err := p.replace(ctx, "node", "first", "second"); err != nil {
		t.Fatal(err)
	}
}

func TestParticipantRetirementCannotRemoveUnrelatedOwner(t *testing.T) {
	p, _ := newParticipantTestInventory(t, 2)
	ctx := context.Background()
	if err := p.enroll(ctx, "node", "first"); err != nil {
		t.Fatal(err)
	}
	if err := p.retire(ctx, "node", "wrong"); !errors.Is(err, ErrParticipantIncarnationConflict) {
		t.Fatalf("wrong incarnation: %v", err)
	}
	if err := p.replace(ctx, "node", "first", "second"); !errors.Is(err, ErrParticipantIncarnationConflict) {
		t.Fatalf("live replacement: %v", err)
	}
	if err := p.replace(ctx, "node", "first", "first"); err == nil {
		t.Fatal("incarnation reuse accepted")
	}
	active, _, err := p.readSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if active["node"] != "first" {
		t.Fatalf("owner changed: %v", active)
	}
}

type participantTransitionEngine struct {
	kvapi.Engine
	before func()
	after  error
	calls  int
}

func (e *participantTransitionEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	e.calls++
	if e.before != nil {
		before := e.before
		e.before = nil
		before()
	}
	committed, err := e.Engine.Txn(ops)
	if err == nil && committed && e.after != nil {
		return false, e.after
	}
	return committed, err
}

func TestParticipantEnrollmentRetriesAcrossRetirementWithoutLosingTombstone(t *testing.T) {
	base, engine := newParticipantTestInventory(t, 2)
	ctx := context.Background()
	if err := base.enroll(ctx, "old", "first"); err != nil {
		t.Fatal(err)
	}
	wrapped := &participantTransitionEngine{Engine: engine, before: func() {
		if err := base.retire(ctx, "old", "first"); err != nil {
			t.Fatal(err)
		}
	}}
	p, err := newParticipantInventory(wrapped, engine.Get, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.enroll(ctx, "new", "second"); err != nil {
		t.Fatal(err)
	}
	if wrapped.calls != 2 {
		t.Fatalf("transaction calls=%d, expected stale CAS then refresh", wrapped.calls)
	}
	if err := p.enroll(ctx, "old", "first"); !errors.Is(err, ErrParticipantRetired) {
		t.Fatalf("lost retirement: %v", err)
	}
}

func TestParticipantRetirementUncertainCommitIsNotReplayed(t *testing.T) {
	base, engine := newParticipantTestInventory(t, 2)
	ctx := context.Background()
	if err := base.enroll(ctx, "node", "first"); err != nil {
		t.Fatal(err)
	}
	uncertain := errors.New("reply lost after commit")
	wrapped := &participantTransitionEngine{Engine: engine, after: uncertain}
	p, err := newParticipantInventory(wrapped, engine.Get, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.retire(ctx, "node", "first"); !errors.Is(err, uncertain) {
		t.Fatalf("uncertain result: %v", err)
	}
	if wrapped.calls != 1 {
		t.Fatalf("uncertain mutation replayed %d times", wrapped.calls)
	}
	// A separate caller-directed resolution observes the committed transition.
	if err := p.retire(ctx, "node", "first"); err != nil {
		t.Fatal(err)
	}
	if wrapped.calls != 1 {
		t.Fatal("idempotent resolution rewrote retirement")
	}
}

func TestParticipantRetirementPreservesCapturedReservationRequirements(t *testing.T) {
	p, engine := newParticipantTestInventory(t, 3)
	ctx := context.Background()
	for node, inc := range map[string]string{"owner": "one", "departing": "old"} {
		if err := p.enroll(ctx, node, inc); err != nil {
			t.Fatal(err)
		}
	}
	owner := mkPID("owner", "process")
	create := func(name string) pendingHeader {
		t.Helper()
		ops, err := p.reservationOps(ctx, pendingHeader{Name: name, PID: owner.String()})
		if err != nil {
			t.Fatal(err)
		}
		committed, err := engine.Txn(ops)
		if err != nil || !committed {
			t.Fatalf("reservation: committed=%v err=%v", committed, err)
		}
		entry, err := engine.Get(pendingKey(name))
		if err != nil {
			t.Fatal(err)
		}
		header, err := decodePending(entry.Value)
		if err != nil {
			t.Fatal(err)
		}
		return header
	}
	create("before")
	if err := p.retire(ctx, "departing", "old"); err != nil {
		t.Fatal(err)
	}
	entry, err := engine.Get(pendingKey("before"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := decodePending(entry.Value)
	if err != nil {
		t.Fatal(err)
	}
	if before.RequiredIncarnations["departing"] != "old" {
		t.Fatal("retirement weakened an existing reservation")
	}
	after := create("after")
	if _, required := after.RequiredIncarnations["departing"]; required {
		t.Fatal("new reservation includes retired participant")
	}
	if err := p.replace(ctx, "departing", "old", "new"); err != nil {
		t.Fatal(err)
	}
	replacement := create("replacement")
	if replacement.RequiredIncarnations["departing"] != "new" {
		t.Fatal("new reservation did not bind replacement incarnation")
	}
}
