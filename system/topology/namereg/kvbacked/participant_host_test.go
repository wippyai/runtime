// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

type registryReleaseCount struct{ count int }

func (r *registryReleaseCount) Release() { r.count++ }

func TestStoppedRegistryRetainsRefusedPackageOwnership(t *testing.T) {
	registry := newStrongReg(t, []string{"node-1"}, 0, nil)
	require.NoError(t, registry.StartReconciler(context.Background()))
	require.NoError(t, registry.StopReconciler(context.Background()))
	pkg := relay.NewServicePackage("sender", "host", "node-1", RegistryHostID, topology.TopicEvents)
	lease := &registryReleaseCount{}
	pkg.Messages[0].SetRetentionLease(lease)
	require.Error(t, registry.Send(pkg))
	require.Equal(t, "sender", pkg.Source.Node)
	require.Zero(t, lease.count, "refusal must leave release to the caller")
	relay.ReleasePackage(pkg)
	require.Equal(t, 1, lease.count)
}

func TestParticipantHostPreservesExitEventsAndRejectsMixedBatch(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	registry := NewService(engine, "node-1", nil, nil)
	owner := mkPID("node-1", "process")
	_, err := registry.RegisterScope(context.Background(), "name", owner, globalapi.Consistent)
	require.NoError(t, err)
	host := &participantHost{registry: registry}
	exit := func() *relay.Package {
		return relay.NewPackage(owner, registry.self, topology.TopicEvents, payload.New(&topology.ExitEvent{From: owner, Kind: topology.Exit}))
	}
	mixed := exit()
	control := relay.AcquireMessage()
	control.Topic = participantSnapshotRequestTopic
	mixed.Messages = append(mixed.Messages, control)
	require.Error(t, host.Send(mixed))
	relay.ReleasePackage(mixed)
	existing, err := registry.Lookup(context.Background(), "name")
	require.NoError(t, err)
	require.True(t, existing.Found, "mixed batch must not partially process its exit event")
	require.NoError(t, host.Send(exit()))
	existing, err = registry.Lookup(context.Background(), "name")
	require.NoError(t, err)
	require.False(t, existing.Found)
}
