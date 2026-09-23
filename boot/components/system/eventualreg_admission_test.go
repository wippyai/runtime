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
)

type unavailableGlobalRegistry struct{ topology.GlobalRegistry }

type eventualLookupStub struct {
	topology.EventualRegistry
	owner pid.PID
	err   error
}

func (s eventualLookupStub) Lookup(context.Context, string, ...global.LookupOption) (global.LookupResult, error) {
	return global.LookupResult{PID: s.owner, Found: s.owner != (pid.PID{})}, s.err
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

func TestStrongUnknownEventualPresenceDoesNotVote(t *testing.T) {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	lookupErr := errors.New("eventual state unavailable")
	ctx = topology.WithEventualRegistry(ctx, eventualLookupStub{err: lookupErr})
	_, conflict, err := (&localPresenceChecker{ctx: ctx}).conflictingClaim("claim", pid.PID{Node: "node-1"})
	if !errors.Is(err, lookupErr) || conflict {
		t.Fatalf("failed eventual read became absence: conflict=%v err=%v", conflict, err)
	}
}
