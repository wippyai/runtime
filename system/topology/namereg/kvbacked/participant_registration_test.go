// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

func TestStrongRegisterUsesCommittedParticipantsWithoutDiscovery(t *testing.T) {
	inventory, authority := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	require.NoError(t, inventory.enroll(ctx, "owner", "one"))
	registry := NewService(authority, "owner", nil, nil)
	registry.ConfigureStrong(StrongDeps{Incarnation: "one", Membership: func() []pid.NodeID { panic("inventory registration consulted discovery") }})
	registry.strong.participants = inventory
	require.NoError(t, registry.startParticipantMember(ctx, inventory, authority, participantSnapshotLimits{MaxEntries: 8, MaxValueBytes: 8192}))
	t.Cleanup(func() { require.NoError(t, registry.StopReconciler(ctx)) })
	owner := mkPID("owner", "process")
	outcome, err := registry.RegisterScope(ctx, "name", owner, globalapi.Strong)
	require.NoError(t, err)
	require.Equal(t, globalapi.RegisterStateActive, outcome.State)
	entry, err := authority.Get(activeKey("name"))
	require.NoError(t, err)
	active, err := decodeActive(entry.Value)
	require.NoError(t, err)
	require.Equal(t, map[pid.NodeID]string{"owner": "one"}, active.RequiredIncarnations)
}

type joinBeforeReservationCommit struct {
	kvapi.Engine
	once     sync.Once
	join     func()
	attempts int
}

func (e *joinBeforeReservationCommit) Txn(ops []kvapi.TxnOp) (bool, error) {
	e.attempts++
	e.once.Do(e.join)
	return e.Engine.Txn(ops)
}

func TestStrongReservationRetriesInventoryConflictWithNewParticipant(t *testing.T) {
	inventory, authority := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	require.NoError(t, inventory.enroll(ctx, "owner", "one"))
	engine := &joinBeforeReservationCommit{Engine: authority, join: func() {
		require.NoError(t, inventory.enroll(ctx, "joining", "two"))
	}}
	registry := NewService(engine, "owner", nil, nil)
	registry.ConfigureStrong(StrongDeps{Incarnation: "one"})
	registry.strong.participants = inventory
	owner := mkPID("owner", "process")
	committed, err := registry.strong.createPending(ctx, pendingHeader{Name: "name", PID: owner.String()})
	require.NoError(t, err)
	require.True(t, committed)
	require.Equal(t, 2, engine.attempts, "a definitive inventory-CAS failure requires fresh preparation")
	entry, err := authority.Get(pendingKey("name"))
	require.NoError(t, err)
	pending, err := decodePending(entry.Value)
	require.NoError(t, err)
	require.Equal(t, map[pid.NodeID]string{"owner": "one", "joining": "two"}, pending.RequiredIncarnations)
}

type uncertainReservationEngine struct {
	kvapi.Engine
	attempts int
}

func (e *uncertainReservationEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	e.attempts++
	if _, err := e.Engine.Txn(ops); err != nil {
		return false, err
	}
	return false, context.DeadlineExceeded // reply lost after authority committed
}

func TestStrongReservationDoesNotRetryUncertainCommit(t *testing.T) {
	inventory, authority := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	require.NoError(t, inventory.enroll(ctx, "owner", "one"))
	engine := &uncertainReservationEngine{Engine: authority}
	registry := NewService(engine, "owner", nil, nil)
	registry.ConfigureStrong(StrongDeps{Incarnation: "one"})
	registry.strong.participants = inventory
	owner := mkPID("owner", "process")
	committed, err := registry.strong.createPending(ctx, pendingHeader{Name: "name", PID: owner.String()})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, committed)
	require.Equal(t, 1, engine.attempts)
	_, err = authority.Get(pendingKey("name"))
	require.NoError(t, err, "uncertainty must not imply rollback or permission to retry")
}
