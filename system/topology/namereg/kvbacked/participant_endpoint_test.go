// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

func TestParticipantEndpointAssemblesMemberAndClient(t *testing.T) {
	_, authority := newParticipantTestInventory(t, 4)
	_, replica := newParticipantTestInventory(t, 4)
	member := NewService(authority, "member", nil, nil)
	member.ConfigureStrong(StrongDeps{Incarnation: "member-one"})
	require.NoError(t, member.ConfigureParticipation(4))
	client := NewService(participantForwardingEngine{Engine: replica, authority: authority}, "client", nil, nil)
	client.SetNonMember(func() bool { return true })
	client.ConfigureStrong(StrongDeps{Incarnation: "client-one", IsLeader: func() bool { return false }})
	require.NoError(t, client.ConfigureParticipation(4))
	config := ParticipantEndpointConfig{MaxEntries: 8, MaxValueBytes: 8192, MaxWireBytes: 16384, MaxConcurrentRequests: 2, MaxRedirects: 2, RequestTimeout: time.Second, RefreshInterval: time.Hour}
	mesh := &participantTestMesh{nodes: make(map[pid.NodeID]relay.ContextSender)}
	ctx := context.Background()
	resolve := func(context.Context) (pid.NodeID, error) { return "member", nil }
	owner, err := NewParticipantEndpoint(ctx, member, mesh, resolve, config)
	require.NoError(t, err)
	viewer, err := NewParticipantEndpoint(ctx, client, mesh, resolve, config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, viewer.Stop(ctx)); require.NoError(t, owner.Stop(ctx)) })
	mesh.nodes["member"], mesh.nodes["client"] = owner, viewer
	require.NoError(t, owner.Start(ctx))
	require.NoError(t, viewer.Start(ctx))
	require.True(t, member.NameReady())
	require.True(t, client.NameReady())
	owned := mkPID("client", "owned")
	other := mkPID("member", "other")
	_, err = client.Register(ctx, "cleanup-owned", owned)
	require.NoError(t, err)
	_, err = member.Register(ctx, "cleanup-other", other)
	require.NoError(t, err)
	require.NoError(t, client.Remove(ctx, owned))
	removed, err := client.Lookup(ctx, "cleanup-owned")
	require.NoError(t, err)
	require.False(t, removed.Found)
	kept, err := member.Lookup(ctx, "cleanup-other")
	require.NoError(t, err)
	require.True(t, kept.Found)
	require.True(t, kept.PID.Equal(other))

	enrolled, _, err := member.strong.participants.readSnapshot()
	require.NoError(t, err)
	require.Equal(t, map[pid.NodeID]string{"member": "member-one", "client": "client-one"}, enrolled)
	require.NoError(t, viewer.Stop(ctx))
	require.False(t, client.NameReady())
	require.ErrorIs(t, viewer.Start(ctx), context.Canceled)
	require.NoError(t, owner.Stop(ctx))
	require.False(t, member.NameReady())
	require.ErrorIs(t, owner.Start(ctx), context.Canceled)
}

func TestParticipantEndpointStopJoinsBlockedStartup(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	service := NewService(engine, "client", nil, nil)
	service.SetNonMember(func() bool { return true })
	service.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
	require.NoError(t, service.ConfigureParticipation(4))
	entered, release := make(chan struct{}), make(chan struct{})
	config := ParticipantEndpointConfig{MaxEntries: 4, MaxValueBytes: 4096, MaxWireBytes: 8192, MaxConcurrentRequests: 1, RequestTimeout: time.Second, RefreshInterval: time.Hour}
	endpoint, err := NewParticipantEndpoint(context.Background(), service, &participantTestMesh{}, func(ctx context.Context) (pid.NodeID, error) { close(entered); <-release; return "", ctx.Err() }, config)
	require.NoError(t, err)
	started := make(chan error, 1)
	go func() { started <- endpoint.Start(context.Background()) }()
	<-entered
	deadline, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	stopErr := endpoint.Stop(deadline)
	// Release the deliberately noncooperative resolver before assertions so a
	// failed expectation cannot leave the fixture's startup goroutine behind.
	close(release)
	require.ErrorIs(t, stopErr, context.DeadlineExceeded)
	require.ErrorIs(t, <-started, context.Canceled)
	require.NoError(t, endpoint.Stop(context.Background()))
	require.False(t, service.NameReady())
}
