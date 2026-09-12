// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
)

// Real Raft and client KV forwarding; transport/provenance is the in-process
// harness. This proves 103-participant Strong attestation, not native scaling.
func TestE2E_StrongIncludesHundredForwardingClients(t *testing.T) {
	if testing.Short() {
		t.Skip("real Raft/100 forwarding clients")
	}
	const clientCount = 100
	c := NewCluster(t, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	config := kvbacked.ParticipantEndpointConfig{MaxEntries: 32, MaxValueBytes: 256 << 10, MaxWireBytes: 512 << 10, MaxConcurrentRequests: 128, MaxRedirects: 4, RequestTimeout: 3 * time.Second, RefreshInterval: 200 * time.Millisecond}
	resolve := func(context.Context) (pid.NodeID, error) {
		if leader := c.Leader(); leader != nil {
			return leader.ID, nil
		}
		return "", fmt.Errorf("no leader")
	}
	endpoints := make(map[string]*kvbacked.ParticipantEndpoint)
	registries := make(map[string]*kvbacked.Service)
	add := func(id string, reg *kvbacked.Service) {
		t.Helper()
		require.NoError(t, reg.ConfigureParticipation(clientCount+3))
		endpoint, err := kvbacked.NewParticipantEndpoint(ctx, reg, participantHarnessSender{self: id, router: c.router}, resolve, config)
		require.NoError(t, err)
		endpoints[id], registries[id] = endpoint, reg
		c.router.register(id, kvbacked.RegistryHostID, endpoint)
		t.Cleanup(func() {
			stop, done := context.WithTimeout(context.Background(), 10*time.Second)
			defer done()
			require.NoError(t, endpoint.Stop(stop))
		})
	}
	for _, node := range c.Nodes() {
		reg := kvbacked.NewService(node.KV, node.ID, nil, nil)
		reg.ConfigureStrong(kvbacked.StrongDeps{Incarnation: node.ID + "-one", IsLeader: node.Raft.IsLeader, Deadline: 5 * time.Second})
		add(node.ID, reg)
	}
	leader := c.Leader()
	require.NoError(t, endpoints[leader.ID].Start(ctx))
	for _, node := range c.Nodes() {
		if node.ID != leader.ID {
			require.NoError(t, endpoints[node.ID].Start(ctx))
		}
	}
	for i := range clientCount {
		id := fmt.Sprintf("consumer-%03d", i)
		reg := c.newClientRegistry(t, id)
		reg.ConfigureStrong(kvbacked.StrongDeps{Incarnation: id + "-one", IsLeader: func() bool { return false }})
		add(id, reg)
		require.NoError(t, endpoints[id].Start(ctx))
	}
	owner := pid.PID{Node: leader.ID, Host: "process", UniqID: "owner"}
	started := time.Now()
	outcome, err := registries[leader.ID].RegisterScope(ctx, "all-consumers", owner, globalapi.Strong)
	require.NoError(t, err)
	require.Equal(t, globalapi.RegisterStateActive, outcome.State)
	t.Logf("Strong promotion with 3 Raft members and 100 forwarding clients: %s", time.Since(started))
	for id, reg := range registries {
		held, found := reg.IsStrongReserved("all-consumers")
		require.True(t, found, "missing exclusion at %s", id)
		require.True(t, held.Equal(owner), "wrong exclusion owner at %s", id)
	}
	missingID := "consumer-099"
	c.mesh.partition(missingID)
	require.Eventually(t, func() bool { return !registries[missingID].NameReady() }, 5*time.Second, 10*time.Millisecond)
	_, err = registries[leader.ID].RegisterScope(ctx, "missing-consumer", owner, globalapi.Strong)
	var missing *globalapi.StrongRegistrationTimeoutError
	require.ErrorAs(t, err, &missing)
	require.Contains(t, missing.MissingAcks, missingID)
	// Consensus operations remain independent of the all-participant barrier.
	_, err = registries[leader.ID].RegisterScope(ctx, "quorum-only", owner, globalapi.Consistent)
	require.NoError(t, err)
	c.mesh.heal(missingID)
	require.Eventually(t, registries[missingID].NameReady, 5*time.Second, 10*time.Millisecond)
	outcome, err = registries[leader.ID].RegisterScope(ctx, "all-consumers-again", owner, globalapi.Strong)
	require.NoError(t, err)
	require.Equal(t, globalapi.RegisterStateActive, outcome.State)
	held, found := registries[missingID].IsStrongReserved("all-consumers-again")
	require.True(t, found)
	require.True(t, held.Equal(owner))
}
