// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// laggedMemberEngine controls the immutable applied view independently of the
// authority source. It models delayed replication without wall-clock sleeps.
type laggedMemberEngine struct {
	kvapi.Engine
	mu      sync.Mutex
	entries map[string]kvapi.Entry
	index   uint64
}

func (e *laggedMemberEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make(map[string]kvapi.Entry)
	for _, key := range keys {
		if entry, ok := e.entries[key]; ok {
			result[key] = entry
		}
	}
	return result, e.index, nil
}

// Standalone KV has versions but no Raft epochs. Model the Raft source's
// epoch stamping explicitly; zero-epoch fixtures cannot test lag ordering.
type epochParticipantSource struct{ participantSnapshotSource }

func (s epochParticipantSource) ScanAtIndex(prefix string, visit func(kvapi.Entry) bool) (uint64, error) {
	return s.participantSnapshotSource.ScanAtIndex(prefix, func(entry kvapi.Entry) bool {
		entry.Epoch = entry.Version
		return visit(entry)
	})
}

func TestParticipantMemberRetainsBootstrapExclusionWhileReplicaLags(t *testing.T) {
	inventory, authority := newParticipantTestInventory(t, 4)
	_, local := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	// Advance the authority beyond the replica's deliberately stale view.
	require.NoError(t, inventory.enroll(ctx, "owner", "one"))
	for range 3 {
		_, err := authority.Set("unrelated", []byte("advance"))
		require.NoError(t, err)
	}
	owner := mkPID("owner", "current")
	body, err := encode(activeValue{Name: "name", PID: owner.String(), Strong: true})
	require.NoError(t, err)
	_, err = authority.Set(activeKey("name"), body)
	require.NoError(t, err)
	var current kvapi.Entry
	_, err = (epochParticipantSource{authority}).ScanAtIndex(activeKey("name"), func(entry kvapi.Entry) bool { current = entry; return true })
	require.NoError(t, err)
	require.Greater(t, current.Epoch, uint64(1))
	replica := &laggedMemberEngine{Engine: local, index: 1}
	service := NewService(replica, "member", nil, nil)
	service.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
	service.strong.participants = inventory
	require.NoError(t, service.startParticipantMember(ctx, inventory, epochParticipantSource{authority}, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}))
	t.Cleanup(func() { require.NoError(t, service.StopReconciler(ctx)) })
	require.True(t, service.NameReady())
	oldOwner := mkPID("owner", "old")
	oldBody, err := encode(activeValue{Name: "name", PID: oldOwner.String(), Strong: true})
	require.NoError(t, err)
	for _, entries := range []map[string]kvapi.Entry{
		nil,
		{activeKey("name"): {Key: activeKey("name"), Value: oldBody, Version: 1, Epoch: 1}},
	} {
		replica.mu.Lock()
		replica.entries = entries
		replica.mu.Unlock()
		require.NoError(t, service.strong.reconcile("name"))
		held, ok := service.IsStrongReserved("name")
		require.True(t, ok, "stale local absence must not release authority exclusion")
		require.True(t, held.Equal(owner), "stale owner must not overwrite authority owner")
	}
	// Once the replica has applied a later deletion, releasing the exclusion is
	// required: retaining it forever would strand the name after recovery.
	replica.mu.Lock()
	replica.entries = nil
	replica.index = current.Epoch + 1
	replica.mu.Unlock()
	require.NoError(t, service.strong.reconcile("name"))
	_, held := service.IsStrongReserved("name")
	require.False(t, held)
}
