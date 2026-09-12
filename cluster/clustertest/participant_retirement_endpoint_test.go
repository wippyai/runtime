// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	topologyapi "github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	localreg "github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
)

// Retirement uses the forwarding client's existing KV engine, not an authority
// fixture mutation or a new wire operation. Real Raft; in-process relay.
func TestE2E_ForwardingParticipantRetiresThroughExistingKV(t *testing.T) {
	if testing.Short() {
		t.Skip("real Raft participant retirement")
	}
	c := NewCluster(t, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config := kvbacked.ParticipantEndpointConfig{MaxEntries: 32, MaxValueBytes: 65536, MaxWireBytes: 131072, MaxConcurrentRequests: 4, MaxRedirects: 4, RequestTimeout: 3 * time.Second, RefreshInterval: 25 * time.Millisecond}
	resolve := func(context.Context) (pid.NodeID, error) {
		if leader := c.Leader(); leader != nil {
			return leader.ID, nil
		}
		return "", fmt.Errorf("no leader")
	}
	endpoints := make(map[string]*kvbacked.ParticipantEndpoint)
	registries := make(map[string]*kvbacked.Service)
	add := func(id string, reg *kvbacked.Service) *kvbacked.ParticipantEndpoint {
		t.Helper()
		require.NoError(t, reg.ConfigureParticipation(4))
		endpoint, err := kvbacked.NewParticipantEndpoint(ctx, reg, participantHarnessSender{self: id, router: c.router}, resolve, config)
		require.NoError(t, err)
		endpoints[id], registries[id] = endpoint, reg
		c.router.register(id, kvbacked.RegistryHostID, endpoint)
		t.Cleanup(func() {
			stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			require.NoError(t, endpoint.Stop(stopCtx))
		})
		return endpoint
	}
	for _, node := range c.Nodes() {
		reg := kvbacked.NewService(node.KV, node.ID, nil, nil)
		reg.ConfigureStrong(kvbacked.StrongDeps{Incarnation: node.ID + "-one", IsLeader: node.Raft.IsLeader, Deadline: 2 * time.Second})
		add(node.ID, reg)
	}
	leader := c.Leader()
	require.NoError(t, endpoints[leader.ID].Start(ctx))
	for _, node := range c.Nodes() {
		if node.ID != leader.ID {
			require.NoError(t, endpoints[node.ID].Start(ctx))
		}
	}
	const clientID = "retiring-client"
	guard := &topologyapi.NameGuard{}
	local := localreg.NewPIDRegistry(localreg.WithNameGuard(guard))
	gossip := eventual.NewService(eventual.Config{LocalNodeID: clientID, NameGuard: guard})
	client := c.newClientRegistry(t, clientID)
	client.ConfigureStrong(kvbacked.StrongDeps{Incarnation: "client-one", NameGuard: guard, IsLeader: func() bool { return false }})
	endpoint := add(clientID, client)
	require.NoError(t, endpoint.Start(ctx))
	clientPID := pid.PID{Node: clientID, Host: "process", UniqID: "owner"}
	_, err := local.Register("local-owned", clientPID)
	require.NoError(t, err)
	_, err = gossip.Register("eventual-owned", clientPID)
	require.NoError(t, err)
	owner := pid.PID{Node: leader.ID, Host: "process", UniqID: "owner"}
	before, err := registries[leader.ID].RegisterScope(ctx, "before-retirement", owner, globalapi.Strong)
	require.NoError(t, err)
	require.Equal(t, globalapi.RegisterStateActive, before.State)
	_, held := client.IsStrongReserved("before-retirement")
	require.True(t, held)
	require.NoError(t, endpoint.Retire(ctx, func(ctx context.Context) error {
		if err := local.WithdrawLocal(ctx); err != nil {
			return err
		}
		return gossip.WithdrawLocal(ctx)
	}))
	require.False(t, client.NameReady())
	_, found := local.LookupLocal("local-owned")
	require.False(t, found)
	entry, err := gossip.Lookup(ctx, "eventual-owned")
	require.NoError(t, err)
	require.False(t, entry.Found)
	_, err = local.Register("late-local", clientPID)
	require.ErrorIs(t, err, topologyapi.ErrNameAdmissionClosed)
	_, err = gossip.Register("late-eventual", clientPID)
	require.ErrorIs(t, err, topologyapi.ErrNameAdmissionClosed)
	require.NoError(t, endpoint.Stop(ctx))
	c.mesh.partition(clientID)
	after, err := registries[leader.ID].RegisterScope(ctx, "after-retirement", owner, globalapi.Strong)
	require.NoError(t, err, "retired client must not be required by a fresh reservation")
	require.Equal(t, globalapi.RegisterStateActive, after.State)
}
