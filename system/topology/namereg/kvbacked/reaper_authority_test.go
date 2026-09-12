// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestReaperWithLaggingReplicaUsesAuthoritativeOwner(t *testing.T) {
	_, authority := newParticipantTestInventory(t, 4)
	_, local := newParticipantTestInventory(t, 4)
	owner := mkPID("client", "old-process")
	registry := NewService(authority, "authority", nil, nil)
	_, err := registry.Register(context.Background(), "restart-name", owner)
	require.NoError(t, err)
	// A follower may have the reverse index but not the active record yet.
	require.NoError(t, authority.Scan(pidIndexBase(owner), func(e kvapi.Entry) bool {
		_, err := local.Set(e.Key, e.Value)
		require.NoError(t, err)
		return true
	}))
	client := NewService(participantForwardingEngine{Engine: local, authority: authority}, "client", nil, nil)
	require.NoError(t, client.Remove(context.Background(), owner))
	_, err = authority.Get(activeKey("restart-name"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound, "exact process removal left an orphaned active claim")
	_, err = authority.Get(pidIndexKey(owner, "restart-name"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
}

func TestReaperPreservesIndexesWhenAuthorityUnavailable(t *testing.T) {
	_, authority := newParticipantTestInventory(t, 4)
	owner := mkPID("client", "owner")
	registry := NewService(authority, "client", nil, nil)
	_, err := registry.Register(context.Background(), "held", owner)
	require.NoError(t, err)
	registry.leaderRead = &unavailableParticipantLeaderRead{}
	require.ErrorIs(t, registry.Remove(context.Background(), owner), errParticipantUnavailable)
	for _, key := range []string{activeKey("held"), pidIndexKey(owner, "held"), nodeIndexKey(owner, "held")} {
		_, err := authority.Get(key)
		require.NoError(t, err, "uncertain read erased %s", key)
	}
}

type reapRaceEngine struct {
	kvapi.Engine
	beforeTxn func()
}

func (e reapRaceEngine) Txn(ops []kvapi.TxnOp) (bool, error) { e.beforeTxn(); return e.Engine.Txn(ops) }

func TestReaperDoesNotEraseRecreatedIndexesAfterAbsentRead(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	owner := mkPID("client", "owner")
	registry := NewService(engine, "client", nil, nil)
	_, err := registry.Register(context.Background(), "recreated", owner)
	require.NoError(t, err)
	require.NoError(t, engine.Delete(activeKey("recreated")))
	registry.engine = reapRaceEngine{Engine: engine, beforeTxn: func() {
		value, err := encode(activeValue{Name: "recreated", PID: owner.String()})
		require.NoError(t, err)
		_, err = engine.Set(activeKey("recreated"), value)
		require.NoError(t, err)
	}}
	require.ErrorIs(t, registry.deleteBinding(owner, "recreated"), kvapi.ErrVersionMismatch)
	for _, key := range []string{activeKey("recreated"), pidIndexKey(owner, "recreated"), nodeIndexKey(owner, "recreated")} {
		_, err := engine.Get(key)
		require.NoError(t, err, "racing registration lost %s", key)
	}
}

type failedReapScan struct{ kvapi.Engine }

func (failedReapScan) Scan(string, func(kvapi.Entry) bool) error { return errParticipantUnavailable }
func TestRemoveReportsIncompleteEnumeration(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	registry := NewService(failedReapScan{Engine: engine}, "client", nil, nil)
	require.ErrorIs(t, registry.Remove(context.Background(), mkPID("client", "owner")), errParticipantUnavailable)
	require.ErrorIs(t, registry.RemoveNode(context.Background(), "client"), errParticipantUnavailable)
}
