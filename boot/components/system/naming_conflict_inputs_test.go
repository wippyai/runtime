// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	localreg "github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

// The embedded interface stubs the unused GlobalRegistry methods in tests.
//
//nolint:govet // Test-only stub; embedding keeps the failure cases focused.
type crossScopeGlobal struct {
	owner       pid.PID
	reserved    pid.PID
	lookupError error
	topology.GlobalRegistry
}

func (g crossScopeGlobal) Lookup(context.Context, string, ...globalapi.LookupOption) (globalapi.LookupResult, error) {
	if g.lookupError != nil {
		return globalapi.LookupResult{}, g.lookupError
	}
	return globalapi.LookupResult{PID: g.owner, Found: g.owner != (pid.PID{})}, nil
}

func (g crossScopeGlobal) IsStrongReserved(string) (pid.PID, bool) {
	return g.reserved, g.reserved != (pid.PID{})
}

func (crossScopeGlobal) NameReady() bool { return true }

func namingContext() context.Context {
	return ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
}

func namingPID(id string) pid.PID { return pid.PID{Node: "node-a", Host: "host", UniqID: id} }

func newCheckedEventual(ctx context.Context, t *testing.T) *eventual.Service {
	t.Helper()
	svc := eventual.NewService(eventual.Config{LocalNodeID: "node-a", CrossScope: newCrossScopeChecker(ctx)})
	require.NoError(t, svc.Start(ctx))
	t.Cleanup(func() { require.NoError(t, svc.Stop()) })
	return svc
}

func TestEventualAdmissionIgnoresComposedOwnBinding(t *testing.T) {
	ctx := namingContext()
	lr := localreg.NewPIDRegistry()
	topology.WithRegistry(ctx, lr)
	svc := newCheckedEventual(ctx, t)
	lr.SetEventualRegistry(svc)
	owner := namingPID("owner")
	_, err := svc.Register("own", owner)
	require.NoError(t, err)
	require.NotEmpty(t, svc.DrainBroadcasts(0, 1<<20))

	_, err = svc.RegisterWithOptions("own", owner, eventual.WithPriority(7))
	require.NoError(t, err)
	frames := svc.DrainBroadcasts(0, 1<<20)
	require.NotEmpty(t, frames, "the own EVENTUAL binding must reach registration instead of short-circuiting")
	var latest eventual.Entry
	for _, frame := range frames {
		entries, _, err := eventual.DecodeFrame(frame[1:])
		require.NoError(t, err)
		for _, entry := range entries {
			latest = entry
		}
	}
	require.Equal(t, uint32(7), latest.Priority)
	require.Equal(t, owner, latest.PID)
}

func TestEventualAdmissionChecksEveryOtherScope(t *testing.T) {
	for _, tc := range []struct {
		name     string
		global   crossScopeGlobal
		local    pid.PID
		want     pid.PID
		accepted bool
	}{
		{name: "local_conflict", local: namingPID("other"), want: namingPID("other")},
		{name: "global_match_local_conflict", global: crossScopeGlobal{owner: namingPID("owner")}, local: namingPID("other"), want: namingPID("other")},
		{name: "global_match_reservation_conflict", global: crossScopeGlobal{owner: namingPID("owner"), reserved: namingPID("other")}, want: namingPID("other")},
		{name: "reservation_match_local_conflict", global: crossScopeGlobal{reserved: namingPID("owner")}, local: namingPID("other"), want: namingPID("other")},
		{name: "strong_reservation_conflict", global: crossScopeGlobal{reserved: namingPID("other")}, want: namingPID("other")},
		{name: "global_match_only", global: crossScopeGlobal{owner: namingPID("owner")}, accepted: true},
		{name: "all_match", global: crossScopeGlobal{owner: namingPID("owner")}, local: namingPID("owner"), accepted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := namingContext()
			if tc.global.owner != (pid.PID{}) || tc.global.reserved != (pid.PID{}) {
				topology.WithGlobalRegistry(ctx, tc.global)
			}
			lr := localreg.NewPIDRegistry()
			if tc.local != (pid.PID{}) {
				_, err := lr.Register("shared", tc.local)
				require.NoError(t, err)
			}
			topology.WithRegistry(ctx, lr)
			svc := newCheckedEventual(ctx, t)
			got, err := svc.Register("shared", namingPID("owner"))
			if tc.accepted {
				require.NoError(t, err)
				require.True(t, got.Equal(namingPID("owner")))
			} else {
				require.ErrorIs(t, err, eventual.ErrNameAlreadyRegistered)
				require.True(t, got.Equal(tc.want))
			}
			res, err := svc.Lookup(ctx, "shared")
			require.NoError(t, err)
			require.False(t, res.Found, "matching other-scope bind and rejected bind must not mint EVENTUAL state")
			require.Empty(t, svc.DrainBroadcasts(0, 1<<20))
		})
	}
}

func TestEventualAdmissionLookupFailureDoesNotClaimName(t *testing.T) {
	sentinel := errors.New("authoritative lookup unavailable")
	ctx := namingContext()
	topology.WithGlobalRegistry(ctx, crossScopeGlobal{lookupError: sentinel})
	lr := localreg.NewPIDRegistry()
	topology.WithRegistry(ctx, lr)
	svc := newCheckedEventual(ctx, t)
	owner := namingPID("owner")
	_, err := svc.Register("unavailable", owner)
	require.ErrorIs(t, err, sentinel)
	res, err := svc.Lookup(ctx, "unavailable")
	require.NoError(t, err)
	require.False(t, res.Found)
	require.Empty(t, svc.DrainBroadcasts(0, 1<<20))
	_, err = lr.Register("unavailable", namingPID("other"))
	require.NoError(t, err)
}

type registryWithoutLocal struct{ topology.PIDRegistry }

func TestEventualAdmissionRejectsUnknownLocalRegistry(t *testing.T) {
	ctx := namingContext()
	topology.WithRegistry(ctx, registryWithoutLocal{})
	svc := newCheckedEventual(ctx, t)
	_, err := svc.Register("unverifiable", namingPID("owner"))
	require.ErrorContains(t, err, "lacks LookupLocal")
	require.Empty(t, svc.DrainBroadcasts(0, 1<<20))
}

func TestStrongPresenceChecksBothWeakerScopes(t *testing.T) {
	owner := namingPID("owner")
	other := namingPID("other")
	for _, tc := range []struct {
		name, local, eventual string
		want                  pid.PID
	}{
		{name: "same_local_different_eventual", local: "owner", eventual: "other", want: other},
		{name: "different_local", local: "other", eventual: "owner", want: other},
		{name: "same_in_both", local: "owner", eventual: "owner"},
		{name: "absent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := namingContext()
			lr := localreg.NewPIDRegistry()
			topology.WithRegistry(ctx, lr)
			if tc.local != "" {
				p := owner
				if tc.local == "other" {
					p = other
				}
				_, err := lr.Register("shared", p)
				require.NoError(t, err)
			}
			if tc.eventual != "" {
				svc := eventual.NewService(eventual.Config{LocalNodeID: "node-a"})
				require.NoError(t, svc.Start(ctx))
				t.Cleanup(func() { require.NoError(t, svc.Stop()) })
				p := owner
				if tc.eventual == "other" {
					p = other
				}
				_, err := svc.Register("shared", p)
				require.NoError(t, err)
				topology.WithEventualRegistry(ctx, svc)
			}
			got, conflict := localConflictForStrong(&localPresenceChecker{ctx: ctx}, "shared", owner.Precomputed())
			require.Equal(t, tc.want != (pid.PID{}), conflict)
			if conflict {
				require.True(t, got.Equal(tc.want))
			}
		})
	}
}
