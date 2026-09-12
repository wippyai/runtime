// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestParticipantBootstrapInstallsPreEnrollmentExclusions(t *testing.T) {
	inventory, authority := newParticipantTestInventory(t, 4)
	_, clientEngine := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	require.NoError(t, inventory.enroll(ctx, "owner-node", "owner-incarnation"))
	owner := mkPID("owner-node", "owner")
	ops, err := inventory.reservationOps(ctx, pendingHeader{Name: "pending", PID: owner.String()})
	require.NoError(t, err)
	committed, err := authority.Txn(ops)
	require.NoError(t, err)
	require.True(t, committed)
	active, err := encode(activeValue{Name: "active", PID: owner.String(), Strong: true})
	require.NoError(t, err)
	_, err = authority.Set(activeKey("active"), active)
	require.NoError(t, err)
	consistent, err := encode(activeValue{Name: "consistent", PID: owner.String()})
	require.NoError(t, err)
	_, err = authority.Set(activeKey("consistent"), consistent)
	require.NoError(t, err)
	client := NewService(clientEngine, "client", nil, nil)
	client.ConfigureStrong(StrongDeps{Incarnation: "client-incarnation", IsLeader: func() bool { return false }})
	revision, err := client.bootstrapParticipant(ctx, inventory, authority, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096})
	require.NoError(t, err)
	require.NotZero(t, revision)
	require.False(t, client.NameReady(), "bootstrap must not open admission before refresh ownership exists")
	for _, name := range []string{"active", "pending"} {
		held, found := client.IsStrongReserved(name)
		require.True(t, found)
		require.True(t, held.Equal(owner))
	}
	_, reserved := client.IsStrongReserved("consistent")
	require.False(t, reserved, "Consistent claims must not become Strong exclusions")
	// No records were materialized into the client's otherwise empty KV replica.
	require.NoError(t, clientEngine.Scan(registryPrefix, func(kvapi.Entry) bool {
		t.Error("bootstrap wrote to the client-local replica")
		return false
	}))
	members, _, err := inventory.readSnapshot()
	require.NoError(t, err)
	require.Equal(t, "client-incarnation", members["client"])
}

func TestParticipantBootstrapFailureRetainsEnrollmentAndClosedAdmission(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "authority-failure", true: "unwithdrawn-conflict"}[conflict], func(t *testing.T) {
			inventory, authority := newParticipantTestInventory(t, 4)
			_, clientEngine := newParticipantTestInventory(t, 4)
			client := NewService(clientEngine, "client", nil, nil)
			deps := StrongDeps{Incarnation: "client-incarnation", IsLeader: func() bool { return false }}
			var source participantSnapshotSource = failedParticipantSnapshotSource{errors.New("authority unavailable")}
			if conflict {
				deps.LocalConflict = func(string, pid.PID) (pid.PID, bool) { return mkPID("client", "conflict"), true }
				owner := mkPID("owner", "owner")
				value, err := encode(activeValue{Name: "name", PID: owner.String(), Strong: true})
				require.NoError(t, err)
				_, err = authority.Set(activeKey("name"), value)
				require.NoError(t, err)
				source = authority
			}
			client.ConfigureStrong(deps)
			revision, err := client.bootstrapParticipant(context.Background(), inventory, source, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096})
			require.Error(t, err)
			require.Zero(t, revision)
			require.False(t, client.NameReady())
			members, _, err := inventory.readSnapshot()
			require.NoError(t, err)
			require.Equal(t, "client-incarnation", members["client"], "failed bootstrap cannot silently retire an enrolled incarnation")
		})
	}
}

func TestParticipantBootstrapRejectsMismatchedMeshIdentity(t *testing.T) {
	for _, identity := range []struct{ node, incarnation string }{{"other", "one"}, {"client", "other"}} {
		t.Run(identity.node+"/"+identity.incarnation, func(t *testing.T) {
			inventory, engine := newParticipantTestInventory(t, 4)
			service := NewService(engine, "client", nil, nil)
			service.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
			source, err := newParticipantClient(context.Background(), identity.node, identity.incarnation,
				func(context.Context) (pid.NodeID, error) {
					t.Fatal("mismatched identity reached resolver")
					return "", nil
				},
				&participantTestMesh{}, 8192, 4, time.Second)
			require.NoError(t, err)
			defer source.stop(context.Background())
			revision, err := service.bootstrapParticipant(context.Background(), inventory, source, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096})
			require.ErrorContains(t, err, "identity")
			require.Zero(t, revision)
			require.False(t, service.NameReady())
			_, _, err = inventory.readSnapshot()
			require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
		})
	}
}
