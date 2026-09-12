// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topologyapi "github.com/wippyai/runtime/api/topology"
)

func TestParticipantEndpointStopJoinsRetirement(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	service := NewService(engine, "member", nil, nil)
	service.ConfigureStrong(StrongDeps{Incarnation: "one", NameGuard: &topologyapi.NameGuard{}})
	require.NoError(t, service.ConfigureParticipation(4))
	mesh := &participantTestMesh{nodes: make(map[pid.NodeID]relay.ContextSender)}
	config := ParticipantEndpointConfig{MaxEntries: 4, MaxValueBytes: 4096, MaxWireBytes: 8192, MaxConcurrentRequests: 1, RequestTimeout: time.Second, RefreshInterval: time.Hour}
	endpoint, err := NewParticipantEndpoint(context.Background(), service, mesh, func(context.Context) (pid.NodeID, error) { return "member", nil }, config)
	require.NoError(t, err)
	mesh.nodes["member"] = endpoint
	t.Cleanup(func() { require.NoError(t, endpoint.Stop(context.Background())) })
	require.NoError(t, endpoint.Start(context.Background()))
	entered, release := make(chan struct{}), make(chan struct{})
	retired := make(chan error, 1)
	go func() {
		retired <- endpoint.Retire(context.Background(), func(ctx context.Context) error {
			close(entered)
			<-release // Deliberately noncooperative; Stop must still join it.
			return ctx.Err()
		})
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	stopErr := endpoint.Stop(ctx)
	close(release) // Unblock before assertions to preserve fixture cleanup.
	require.ErrorIs(t, stopErr, context.DeadlineExceeded)
	require.ErrorIs(t, <-retired, context.Canceled)
	require.NoError(t, endpoint.Stop(context.Background()))
	require.ErrorIs(t, endpoint.Retire(context.Background(), func(context.Context) error { t.Error("withdrawal after stop"); return nil }), context.Canceled)
	active, _, err := service.strong.participants.readSnapshot()
	require.NoError(t, err)
	require.Equal(t, "one", active["member"], "canceled withdrawal must retain enrollment")
	require.False(t, service.NameReady())
}

func TestParticipantEndpointRetireRequiresRunningLifecycle(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	service := NewService(engine, "member", nil, nil)
	guard := &topologyapi.NameGuard{}
	service.ConfigureStrong(StrongDeps{Incarnation: "one", NameGuard: guard})
	require.NoError(t, service.ConfigureParticipation(4))
	config := ParticipantEndpointConfig{MaxEntries: 4, MaxValueBytes: 4096, MaxWireBytes: 8192, MaxConcurrentRequests: 1, RequestTimeout: time.Second, RefreshInterval: time.Hour}
	endpoint, err := NewParticipantEndpoint(context.Background(), service, &participantTestMesh{}, func(context.Context) (pid.NodeID, error) { return "member", nil }, config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, endpoint.Stop(context.Background())) })
	require.Error(t, endpoint.Retire(context.Background(), func(context.Context) error { t.Error("withdrawal before startup"); return nil }))
	unlock, err := guard.LockContext(context.Background(), "still-open")
	require.NoError(t, err)
	unlock()
}
