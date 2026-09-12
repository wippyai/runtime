// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	topologyapi "github.com/wippyai/runtime/api/topology"
	localreg "github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

func TestParticipantRetirementComposesActualWeakerRegistries(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	guard := &topologyapi.NameGuard{}
	local := localreg.NewPIDRegistry(localreg.WithNameGuard(guard))
	gossip := eventual.NewService(eventual.Config{LocalNodeID: "member", NameGuard: guard})
	service := NewService(engine, "member", nil, nil)
	service.ConfigureStrong(StrongDeps{Incarnation: "one", NameGuard: guard, IsLeader: func() bool { return false }})
	service.strong.participants = inventory
	ctx := context.Background()
	require.NoError(t, service.startParticipantMember(ctx, inventory, engine, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}))
	t.Cleanup(func() { require.NoError(t, service.StopReconciler(ctx)) })
	owner := mkPID("member", "process")
	_, err := local.Register("local", owner)
	require.NoError(t, err)
	_, err = gossip.Register("eventual", owner)
	require.NoError(t, err)
	withdraw := func(ctx context.Context) error {
		// Inventory must retain us until both weaker registries have withdrawn.
		active, _, err := inventory.readSnapshot()
		if err != nil {
			return err
		}
		if active["member"] != "one" {
			return errors.New("inventory retirement preceded local withdrawal")
		}
		if err := local.WithdrawLocal(ctx); err != nil {
			return err
		}
		return gossip.WithdrawLocal(ctx)
	}
	require.NoError(t, service.retireParticipant(ctx, withdraw))
	_, found := local.LookupLocal("local")
	require.False(t, found)
	binding, err := gossip.Lookup(ctx, "eventual")
	require.NoError(t, err)
	require.False(t, binding.Found)
	require.False(t, service.NameReady())
	require.NoError(t, service.reconciler.Load().ctx.Err(), "retirement does not stop attestation")
	active, _, err := inventory.readSnapshot()
	require.NoError(t, err)
	require.NotContains(t, active, "member")
	require.ErrorIs(t, inventory.enroll(ctx, "member", "one"), ErrParticipantRetired)
	_, err = local.Register("new-local", owner)
	require.ErrorIs(t, err, topologyapi.ErrNameAdmissionClosed)
	_, err = gossip.Register("new-eventual", owner)
	require.ErrorIs(t, err, topologyapi.ErrNameAdmissionClosed)
	require.NoError(t, service.StopReconciler(ctx))
}

func TestParticipantWithdrawalFailureRetainsInventoryAndSealsAdmission(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	guard := &topologyapi.NameGuard{}
	service := NewService(engine, "member", nil, nil)
	service.ConfigureStrong(StrongDeps{Incarnation: "one", NameGuard: guard, IsLeader: func() bool { return false }})
	service.strong.participants = inventory
	ctx := context.Background()
	require.NoError(t, service.startParticipantMember(ctx, inventory, engine, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}))
	t.Cleanup(func() { require.NoError(t, service.StopReconciler(ctx)) })
	cause := errors.New("withdrawal interrupted")
	retirementErr := service.retireParticipant(ctx, func(context.Context) error { return cause })
	require.ErrorIs(t, retirementErr, cause)
	require.ErrorContains(t, retirementErr, "withdraw weaker names")
	active, _, err := inventory.readSnapshot()
	require.NoError(t, err)
	require.Equal(t, "one", active["member"])
	require.False(t, service.NameReady())
	_, err = guard.LockContext(ctx, "late")
	require.ErrorIs(t, err, topologyapi.ErrNameAdmissionClosed)
	require.NoError(t, service.retireParticipant(ctx, func(context.Context) error { return nil }))
	require.NoError(t, service.retireParticipant(ctx, func(context.Context) error { return nil }))
}

func TestParticipantSealedGuardStillAllowsBoundAttestation(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	guard := &topologyapi.NameGuard{}
	service := NewService(engine, "member", nil, nil)
	service.ConfigureStrong(StrongDeps{Incarnation: "one", NameGuard: guard, IsLeader: func() bool { return false }, LocalConflict: func(name string, owner pid.PID) (pid.PID, bool) { return mkPID("member", "other"), name == "conflict" }})
	service.strong.participants = inventory
	ctx := context.Background()
	require.NoError(t, service.startParticipantMember(ctx, inventory, engine, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}))
	t.Cleanup(func() { require.NoError(t, service.StopReconciler(ctx)) })
	require.NoError(t, service.sealParticipantMutations(ctx))
	require.NoError(t, guard.Close(ctx))
	owner := mkPID("member", "p")

	for _, name := range []string{"pending", "conflict"} {
		header := pendingHeader{Name: name, PID: owner.String()}
		ops, err := inventory.reservationOps(ctx, header)
		require.NoError(t, err)
		committed, err := engine.Txn(ops)
		require.NoError(t, err)
		require.True(t, committed)
		entry, err := engine.Get(pendingKey(name))
		require.NoError(t, err)
		header, err = decodePending(entry.Value)
		require.NoError(t, err)
		require.NoError(t, service.strong.attestHeaderContext(ctx, name, entry.Epoch, owner, header))
		key := header.ackKey(name, entry.Epoch, "member")
		if name == "conflict" {
			key = header.rejectKey(name, entry.Epoch, "member")
		}
		_, err = engine.Get(key)
		require.NoError(t, err, "sealed admission must preserve actual conflict attestation")
	}
	require.False(t, service.NameReady())
}
