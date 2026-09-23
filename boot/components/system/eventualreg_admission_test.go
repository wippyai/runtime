// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"errors"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	topology "github.com/wippyai/runtime/api/topology"
	global "github.com/wippyai/runtime/api/topology/namereg/global"
	systopology "github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

type unavailableGlobalRegistry struct{ topology.GlobalRegistry }

type eventualLookupStub struct {
	topology.EventualRegistry
	err        error
	owner      pid.PID
	localClaim pid.PID
}

type winnerOnlyEventualLookupStub struct{ topology.EventualRegistry }

func (winnerOnlyEventualLookupStub) Lookup(context.Context, string, ...global.LookupOption) (global.LookupResult, error) {
	return global.LookupResult{}, nil
}

func (s eventualLookupStub) Lookup(context.Context, string, ...global.LookupOption) (global.LookupResult, error) {
	return global.LookupResult{PID: s.owner, Found: s.owner != (pid.PID{})}, s.err
}

func (s eventualLookupStub) ConflictingLiveClaim(_ string, proposed pid.PID) (pid.PID, bool, error) {
	if s.err != nil {
		return pid.PID{}, false, s.err
	}
	if s.localClaim != (pid.PID{}) && !s.localClaim.Equal(proposed) {
		return s.localClaim, true, nil
	}
	if s.owner != (pid.PID{}) && !s.owner.Equal(proposed) {
		return s.owner, true, nil
	}
	return pid.PID{}, false, nil
}

var errGlobalLookupUnavailable = errors.New("global lookup unavailable")

func (unavailableGlobalRegistry) Lookup(context.Context, string, ...global.LookupOption) (global.LookupResult, error) {
	return global.LookupResult{}, errGlobalLookupUnavailable
}

func TestEventualCrossScopeReadFailureIsNotAbsence(t *testing.T) {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx = topology.WithGlobalRegistry(ctx, unavailableGlobalRegistry{})
	owner, found, err := newCrossScopeChecker(ctx).LookupOther("claim", pid.PID{})
	if !errors.Is(err, errGlobalLookupUnavailable) || found || owner != (pid.PID{}) {
		t.Fatalf("failed global lookup admitted other scope: owner=%v found=%v err=%v", owner, found, err)
	}
}

func TestStrongChecksEventualEvenWhenLocalMatchesClaimant(t *testing.T) {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	claimant := pid.PID{Node: "node-1", Host: "host", UniqID: "claimant"}
	other := pid.PID{Node: "node-1", Host: "host", UniqID: "other"}
	local := systopology.NewPIDRegistry()
	if _, err := local.Register("claim", claimant); err != nil {
		t.Fatal(err)
	}
	ctx = topology.WithRegistry(ctx, local)
	ctx = topology.WithEventualRegistry(ctx, eventualLookupStub{owner: other})
	got, conflict, err := (&localPresenceChecker{ctx: ctx}).conflictingClaim("claim", claimant)
	if err != nil || !conflict || !got.Equal(other) {
		t.Fatalf("matching LOCAL hid conflicting EVENTUAL: owner=%v conflict=%v err=%v", got, conflict, err)
	}
}

func TestStrongChecksHiddenLocalEventualClaim(t *testing.T) {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	claimant := pid.PID{Node: "node-2", Host: "host", UniqID: "winner"}
	hidden := pid.PID{Node: "node-1", Host: "host", UniqID: "loser"}
	ctx = topology.WithEventualRegistry(ctx, eventualLookupStub{owner: claimant, localClaim: hidden})
	got, conflict, err := (&localPresenceChecker{ctx: ctx}).conflictingClaim("claim", claimant)
	if err != nil || !conflict || !got.Equal(hidden) {
		t.Fatalf("hidden local EVENTUAL claim escaped Strong vote: owner=%v conflict=%v err=%v", got, conflict, err)
	}
}

func TestStrongChecksHiddenLocalEventualClaimAfterGossip(t *testing.T) {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	a := eventual.NewService(eventual.Config{LocalNodeID: "node-A"})
	b := eventual.NewService(eventual.Config{LocalNodeID: "node-B"})
	hidden := pid.PID{Node: "node-A", Host: "host", UniqID: "hidden"}
	winner := pid.PID{Node: "node-B", Host: "host", UniqID: "winner"}
	if _, err := a.Register("claim", hidden); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RegisterWithOptions("claim", winner, eventual.WithPriority(1)); err != nil {
		t.Fatal(err)
	}
	for _, frame := range b.DrainBroadcasts(0, 1<<20) {
		a.OnFrame(frame)
	}
	if got, err := a.Lookup(ctx, "claim"); err != nil || !got.Found || !got.PID.Equal(winner) {
		t.Fatalf("visible EVENTUAL winner = %+v, err=%v; want B", got, err)
	}
	ctx = topology.WithEventualRegistry(ctx, a)
	if got, conflict, err := (&localPresenceChecker{ctx: ctx}).conflictingClaim("claim", winner); err != nil || !conflict || !got.Equal(hidden) {
		t.Fatalf("Strong vote missed hidden A: owner=%v conflict=%v err=%v", got, conflict, err)
	}
	if !b.Unregister("claim") {
		t.Fatal("B did not unregister its own dot")
	}
	for _, frame := range b.DrainBroadcasts(0, 1<<20) {
		a.OnFrame(frame)
	}
	if got, err := a.Lookup(ctx, "claim"); err != nil || !got.Found || !got.PID.Equal(hidden) {
		t.Fatalf("hidden dot did not reappear as expected: %+v, err=%v", got, err)
	}
}

func TestStrongUnknownEventualPresenceDoesNotVote(t *testing.T) {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	lookupErr := errors.New("eventual state unavailable")
	ctx = topology.WithEventualRegistry(ctx, eventualLookupStub{err: lookupErr})
	_, conflict, err := (&localPresenceChecker{ctx: ctx}).conflictingClaim("claim", pid.PID{Node: "node-1"})
	if !errors.Is(err, lookupErr) || conflict {
		t.Fatalf("failed eventual read became absence: conflict=%v err=%v", conflict, err)
	}
}

func TestStrongFailsClosedWithoutEventualLocalClaimLookup(t *testing.T) {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx = topology.WithEventualRegistry(ctx, winnerOnlyEventualLookupStub{})
	_, conflict, err := (&localPresenceChecker{ctx: ctx}).conflictingClaim("claim", pid.PID{Node: "node-1"})
	if err == nil || conflict {
		t.Fatalf("winner-only EVENTUAL registry admitted Strong: conflict=%v err=%v", conflict, err)
	}
}
