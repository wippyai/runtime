// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type cacheTrackingMesh struct {
	participantTestMesh
	full, unchanged atomic.Int64
	tamper          atomic.Int32
}

func (m *cacheTrackingMesh) SendContext(ctx context.Context, pkg *relay.Package) error {
	if len(pkg.Messages) == 1 && pkg.Messages[0].Topic == participantSnapshotResponseTopic {
		response, err := decodeParticipantResponse(pkg.Messages[0].Payloads[0].Data().([]byte), 8192, 8)
		if err != nil {
			return err
		}
		if response.Unchanged {
			m.unchanged.Add(1)
			switch m.tamper.Load() {
			case 1:
				response.Lifetime += "wrong"
			case 2:
				response.Revision++
			}
			if m.tamper.Load() != 0 {
				body, err := encodeParticipantResponse(response, 8192, 8)
				if err != nil {
					return err
				}
				pkg.Messages[0].Payloads[0] = payload.New(body)
			}
		} else {
			m.full.Add(1)
		}
	}
	return m.participantTestMesh.SendContext(ctx, pkg)
}

func TestParticipantCacheReuseIsolationAndAuthorityReplacement(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	mesh := &cacheTrackingMesh{participantTestMesh: participantTestMesh{nodes: make(map[pid.NodeID]relay.ContextSender)}}
	newReceiver := func() *participantReceiver {
		authority, err := newParticipantAuthority(ctx, "authority", inventory, engine, participantSnapshotLimits{8, 8192}, 2)
		require.NoError(t, err)
		receiver, err := newParticipantReceiver(authority, mesh, 8192, 8, time.Second)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, receiver.stop(ctx)) })
		return receiver
	}
	receiver := newReceiver()
	client, err := newParticipantClient(ctx, "client", "one", func(context.Context) (pid.NodeID, error) { return "authority", nil }, mesh, 8192, 8, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.stop(ctx)) })
	mesh.nodes["authority"], mesh.nodes["client"] = receiver, client
	// Partial consumption must not install a new cache.
	_, err = client.ScanAtIndex(registryPrefix, func(kvapi.Entry) bool { return false })
	require.NoError(t, err)
	require.Nil(t, client.cached)
	// Cancellation from inside the visitor also prevents installation.
	canceled, cancel := context.WithCancel(ctx)
	_, err = client.ScanAtIndexContext(canceled, registryPrefix, func(kvapi.Entry) bool { cancel(); return true })
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, client.cached)
	limits := participantSnapshotLimits{8, 8192}
	full, err := inventory.captureSnapshot(ctx, client, "client", "one", limits)
	require.NoError(t, err)
	require.NotNil(t, client.cached)
	original := append([]byte(nil), full.Entries[participantsKey].Value...)
	full.Entries[participantsKey].Value[0] ^= 0xff
	replay, err := inventory.captureSnapshot(ctx, client, "client", "one", limits)
	require.NoError(t, err)
	require.Equal(t, original, replay.Entries[participantsKey].Value)
	require.EqualValues(t, 1, mesh.unchanged.Load())
	// Mutation to an unchanged replay must not corrupt the next replay either.
	replay.Entries[participantsKey].Value[0] ^= 0xff
	_, err = inventory.captureSnapshot(ctx, client, "client", "one", limits)
	require.NoError(t, err)
	require.EqualValues(t, 2, mesh.unchanged.Load())
	for _, mode := range []int32{1, 2} {
		mesh.tamper.Store(mode)
		_, err = inventory.captureSnapshot(ctx, client, "client", "one", limits)
		require.ErrorIs(t, err, errParticipantWireInvalid)
	}
	mesh.tamper.Store(0)
	_, err = inventory.captureSnapshot(ctx, client, "client", "one", limits)
	require.NoError(t, err)
	before := mesh.full.Load()
	require.NoError(t, receiver.stop(ctx))
	mesh.nodes["authority"] = newReceiver()
	_, err = inventory.captureSnapshot(ctx, client, "client", "one", limits)
	require.NoError(t, err)
	require.Equal(t, before+1, mesh.full.Load(), "new lifetime with unchanged numeric revision must send full state")
	require.NoError(t, inventory.retire(ctx, "client", "one"))
	_, err = inventory.captureSnapshot(ctx, client, "client", "one", limits)
	require.ErrorIs(t, err, ErrParticipantRetired)
	require.NoError(t, client.stop(ctx))
	require.Nil(t, client.cached)
}

func TestParticipantCacheCountsInventoryMembersSeparatelyFromKeys(t *testing.T) {
	members := map[pid.NodeID]string{"client": "one"}
	for i := 0; i < 9; i++ {
		members[pid.NodeID(fmt.Sprintf("peer-%d", i))] = "one"
	}
	body, err := encode(members)
	require.NoError(t, err)
	ctx := context.Background()
	client, err := newParticipantClient(ctx, "client", "one", func(context.Context) (pid.NodeID, error) { return "authority", nil }, &participantTestMesh{}, 8192, 1, time.Second)
	require.NoError(t, err)
	defer client.stop(ctx)
	response := &participantWireResponse{Version: 1, Correlation: 1, Lifetime: "authority-one", Incarnation: "one", Revision: 1, Entries: []kvapi.Entry{{Key: participantsKey, Value: body, Version: 1, Epoch: 1}}}
	require.True(t, client.cacheableSnapshot(ctx, response), "one KV key can contain many inventory participants")
	client.maxParticipants = 9
	require.False(t, client.cacheableSnapshot(ctx, response), "explicit participant limit must still be enforced")
	client.maxParticipants = 10
	require.True(t, client.cacheableSnapshot(ctx, response))
}
