// SPDX-License-Identifier: MPL-2.0
package clustertest

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
	"go.uber.org/zap"
)

// The harness uses real Raft and KV, but this adapter models connection-derived
// provenance. Native authenticated TLS is tested separately in kvbacked.
type participantHarnessSender struct {
	self     pid.NodeID
	router   *relayRouter
	observed *sync.Map
}

func (s participantHarnessSender) SendContext(ctx context.Context, pkg *relay.Package) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if pkg.Source.Node != s.self {
		return fmt.Errorf("harness peer mismatch")
	}
	observe := s.observed != nil && pkg.Target.Node == "participant-client" && len(pkg.Messages) == 1 && pkg.Messages[0].Topic == "naming.snapshot.response"
	pkg.ReceivedFrom = s.self
	err := s.router.Send(pkg)
	if err == nil && observe {
		s.observed.Store(s.self, true)
	}
	return err
}

func TestE2E_ParticipantEndpointsIncludeForwardingClient(t *testing.T) {
	if testing.Short() {
		t.Skip("real Raft participation integration")
	}
	c := NewCluster(t, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	config := kvbacked.ParticipantEndpointConfig{MaxEntries: 64, MaxValueBytes: 65536, MaxWireBytes: 131072, MaxConcurrentRequests: 4, MaxRedirects: 4, RequestTimeout: 3 * time.Second, RefreshInterval: 25 * time.Millisecond}
	registrant := c.Follower()
	observed := &sync.Map{}
	regs := make(map[string]*kvbacked.Service)
	endpoints := make(map[string]*kvbacked.ParticipantEndpoint)
	t.Cleanup(func() {
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		for _, endpoint := range endpoints {
			if err := endpoint.Stop(stop); err != nil {
				t.Error(err)
			}
		}
	})
	resolve := func(context.Context) (pid.NodeID, error) {
		if leader := c.Leader(); leader != nil {
			return leader.ID, nil
		}
		return "", fmt.Errorf("no leader")
	}
	for _, node := range c.Nodes() {
		reg := kvbacked.NewService(node.KV, node.ID, nil, zap.NewExample())
		deadline := 2 * time.Second
		if node.ID == registrant.ID {
			deadline = 15 * time.Second
		}
		reg.ConfigureStrong(kvbacked.StrongDeps{Incarnation: node.ID + "-one", IsLeader: node.Raft.IsLeader, Deadline: deadline})
		require.NoError(t, reg.ConfigureParticipation(8))
		endpoint, err := kvbacked.NewParticipantEndpoint(ctx, reg, participantHarnessSender{node.ID, c.router, observed}, resolve, config)
		require.NoError(t, err)
		regs[node.ID], endpoints[node.ID] = reg, endpoint
		c.router.register(node.ID, kvbacked.RegistryHostID, endpoint)
	}
	leader := c.Leader()
	require.NoError(t, endpoints[leader.ID].Start(ctx))
	for _, node := range c.Nodes() {
		if node.ID != leader.ID {
			require.NoError(t, endpoints[node.ID].Start(ctx))
		}
	}
	clientID := "participant-client"
	client := c.newClientRegistry(t, clientID)
	client.ConfigureStrong(kvbacked.StrongDeps{Incarnation: "client-one", IsLeader: func() bool { return false }})
	require.NoError(t, client.ConfigureParticipation(8))
	endpoint, err := kvbacked.NewParticipantEndpoint(ctx, client, participantHarnessSender{clientID, c.router, observed}, resolve, config)
	require.NoError(t, err)
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := endpoint.Stop(stop); err != nil {
			t.Error(err)
		}
	})
	endpoints[clientID] = endpoint
	c.router.register(clientID, kvbacked.RegistryHostID, endpoint)
	require.NoError(t, endpoint.Start(ctx))
	require.True(t, client.NameReady())
	owner := pid.PID{Node: leader.ID, Host: "process", UniqID: "owner"}
	outcome, err := regs[leader.ID].RegisterScope(ctx, "enrolled-client", owner, globalapi.Strong)
	require.NoError(t, err)
	require.Equal(t, globalapi.RegisterStateActive, outcome.State)
	held, ok := client.IsStrongReserved("enrolled-client")
	require.True(t, ok, "promotion requires the forwarding client's exclusion ACK")
	require.True(t, held.Equal(owner))

	// A connected process can be partitioned without being retired. Repeated
	// heal must restore its feed, not create another incarnation or bypass ACKs.
	for cycle := 0; cycle < 2; cycle++ {
		c.mesh.partition(clientID)
		require.Eventually(t, func() bool { return !client.NameReady() }, 4*time.Second, 10*time.Millisecond)
		held, ok := client.IsStrongReserved("enrolled-client")
		require.True(t, ok)
		require.True(t, held.Equal(owner))
		_, err := regs[leader.ID].RegisterScope(ctx, fmt.Sprintf("partitioned-%d", cycle), owner, globalapi.Strong)
		var timeout *globalapi.StrongRegistrationTimeoutError
		require.ErrorAs(t, err, &timeout)
		require.Contains(t, timeout.MissingAcks, clientID)
		c.mesh.heal(clientID)
		require.Eventually(t, client.NameReady, 4*time.Second, 10*time.Millisecond)
		resumed, err := regs[leader.ID].RegisterScope(ctx, fmt.Sprintf("healed-%d", cycle), owner, globalapi.Strong)
		require.NoError(t, err)
		require.Equal(t, globalapi.RegisterStateActive, resumed.State)
	}

	// Keep an admitted reservation pending across leader loss by withholding
	// the client's vote. All server votes must commit before the crash: a dead
	// server's prior exact-incarnation acknowledgement remains valid evidence.
	c.mesh.partition(clientID)
	require.Eventually(t, func() bool { return !client.NameReady() }, 4*time.Second, 10*time.Millisecond)
	pendingOwner := pid.PID{Node: registrant.ID, Host: "process", UniqID: "in-flight"}
	type registrationResult struct {
		outcome globalapi.RegisterOutcome
		err     error
	}
	result := make(chan registrationResult, 1)
	registrationDone := make(chan struct{})
	defer func() {
		cancel()
		select {
		case <-registrationDone:
		case <-time.After(5 * time.Second):
			t.Error("in-flight registration did not join cancellation")
		}
	}()
	go func() {
		defer close(registrationDone)
		outcome, err := regs[registrant.ID].RegisterScope(ctx, "pending-across-loss", pendingOwner, globalapi.Strong)
		result <- registrationResult{outcome, err}
	}()
	var pending kvapi.Entry
	require.Eventually(t, func() bool {
		var err error
		pending, err = leader.KV.GetViaLeader("_sys:registry:pending:pending-across-loss")
		if err != nil {
			return false
		}
		enc := base64.RawURLEncoding.EncodeToString
		for _, node := range c.Nodes() {
			key := "_sys:registry:participant_ack:" + enc([]byte("pending-across-loss")) + ":" + strconv.FormatUint(pending.Epoch, 10) + ":" + enc([]byte(node.ID)) + ":" + enc([]byte(node.ID+"-one"))
			if _, err := leader.KV.GetViaLeader(key); err != nil {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)

	// Losing the leader must not erase the committed exclusion. A replacement
	// leader can serve the client feed, but retirement is not inferred from loss.
	require.NoError(t, endpoints[leader.ID].Stop(ctx))
	for index, node := range c.Nodes() {
		if node.ID == leader.ID {
			c.Kill(index)
			break
		}
	}
	replacement := c.WaitLeader(10 * time.Second)
	require.NotEqual(t, leader.ID, replacement.ID)
	inherited, err := replacement.KV.GetViaLeader(pending.Key)
	require.NoError(t, err)
	require.Equal(t, pending.Value, inherited.Value, "leader loss must not rewrite the committed attempt or its required incarnations")
	require.Equal(t, pending.Epoch, inherited.Epoch)
	select {
	case completed := <-result:
		t.Fatalf("claim completed while required client remained partitioned: %+v", completed)
	default:
	}
	c.mesh.heal(clientID)
	select {
	case completed := <-result:
		require.NoError(t, completed.err)
		require.Equal(t, globalapi.RegisterStateActive, completed.outcome.State)
		require.True(t, completed.outcome.PID.Equal(pendingOwner))
	case <-ctx.Done():
		t.Fatal("in-flight claim did not resume after client healed")
	}

	require.Eventually(t, func() bool {
		_, refreshed := observed.Load(replacement.ID)
		held, ok := client.IsStrongReserved("enrolled-client")
		if !refreshed || !ok || !held.Equal(owner) || !client.NameReady() {
			return false
		}
		result, err := client.Lookup(ctx, "enrolled-client")
		return err == nil && result.Found && result.PID.Equal(owner)
	}, 5*time.Second, 25*time.Millisecond)

	// Discovery loss cannot shrink the committed Strong electorate. The timeout
	// must identify the missing incarnation's node; quorum-backed Consistent
	// registration remains available independently of that all-participant barrier.
	nextOwner := pid.PID{Node: replacement.ID, Host: "process", UniqID: "after-loss"}
	_, err = regs[replacement.ID].RegisterScope(ctx, "strong-after-loss", nextOwner, globalapi.Strong)
	var missing *globalapi.StrongRegistrationTimeoutError
	require.ErrorAs(t, err, &missing)
	require.Contains(t, missing.MissingAcks, leader.ID)
	_, err = regs[replacement.ID].RegisterScope(ctx, "consistent-after-loss", nextOwner, globalapi.Consistent)
	require.NoError(t, err)
	found, err := client.Lookup(ctx, "consistent-after-loss")
	require.NoError(t, err)
	require.True(t, found.Found)
	require.True(t, found.PID.Equal(nextOwner))
}
