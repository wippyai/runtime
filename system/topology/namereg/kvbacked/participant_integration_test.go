// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	topapi "github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	local "github.com/wippyai/runtime/system/topology"
)

// This composes real KV engines, Strong registration, the authority watch,
// client refresh, and LOCAL admission. Forwarding is transport-free; native
// wire, Raft failover, and independent-process acceptance are separate gates.
func TestParticipantFeedControlsStrongPromotionAndLocalAdmission(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "promote", true: "reject-local-conflict"}[conflict], func(t *testing.T) {
			inventory, authority := newParticipantTestInventory(t, 4)
			_, replica := newParticipantTestInventory(t, 4)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ownerRegistry := NewService(authority, "owner", nil, nil)
			ownerRegistry.ConfigureStrong(StrongDeps{Incarnation: "owner-incarnation"})
			require.NoError(t, ownerRegistry.ConfigureParticipation(4))
			inventory = ownerRegistry.strong.participants
			require.NoError(t, ownerRegistry.startParticipantMember(ctx, inventory, authority, participantSnapshotLimits{MaxEntries: 8, MaxValueBytes: 8192}))
			guard := &topapi.NameGuard{}
			client := NewService(participantForwardingEngine{Engine: replica, authority: authority}, "client", nil, nil)
			client.SetNonMember(func() bool { return true })
			locals := local.NewPIDRegistry(local.WithNameGuard(guard), local.WithGlobalRegistry(client))
			client.ConfigureStrong(StrongDeps{Incarnation: "client-incarnation", NameGuard: guard, IsLeader: func() bool { return false }, LocalConflict: func(name string, _ pid.PID) (pid.PID, bool) { return locals.LookupLocal(name) }})
			t.Cleanup(func() {
				cancel()
				stop, done := context.WithTimeout(context.Background(), time.Second)
				defer done()
				require.NoError(t, client.StopReconciler(stop))
				require.NoError(t, ownerRegistry.StopReconciler(stop))
			})
			require.NoError(t, client.startParticipantFeed(ctx, inventory, authority, participantSnapshotLimits{MaxEntries: 8, MaxValueBytes: 8192}, 5*time.Millisecond))
			weak := mkPID("client", "local-app")
			if conflict {
				_, err := locals.Register("name", weak)
				require.NoError(t, err)
			}
			owner := mkPID("owner", "process")
			operation, done := context.WithTimeout(ctx, 2*time.Second)
			defer done()
			outcome, err := ownerRegistry.RegisterScope(operation, "name", owner, globalapi.Strong)
			if conflict {
				var rejected *globalapi.StrongConflictError
				require.ErrorAs(t, err, &rejected)
				kept, found := locals.LookupLocal("name")
				require.True(t, found)
				require.True(t, kept.Equal(weak), "a rejected Strong claim must not revoke the existing app name")
			} else {
				require.NoError(t, err)
				require.Equal(t, globalapi.RegisterStateActive, outcome.State)
				_, err = locals.Register("name", weak)
				require.Error(t, err, "client must retain its exclusion after acknowledging")
				held, found := client.IsStrongReserved("name")
				require.True(t, found)
				require.True(t, held.Equal(owner))
			}
		})
	}
}
