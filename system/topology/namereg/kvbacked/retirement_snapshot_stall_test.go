// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	topapi "github.com/wippyai/runtime/api/topology"
)

type acceptedSnapshotWithoutReply struct{ sent chan struct{} }

func (s *acceptedSnapshotWithoutReply) SendContext(_ context.Context, pkg *relay.Package) error {
	relay.ReleasePackage(pkg)
	close(s.sent)
	return nil
}

// Model a background authority read holding the same refresh gate cleanup uses.
// Cleanup cancels the stale read and obtains fresh evidence; it never invents
// absence or abandons its accepted mutation.
func TestCleanupPreemptsStalledBackgroundSnapshotBeforeRetirement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, engine := newParticipantTestInventory(t, 4)
		service := NewService(engine, "member", nil, nil)
		service.ConfigureStrong(StrongDeps{Incarnation: "one", NameGuard: &topapi.NameGuard{}})
		require.NoError(t, service.ConfigureParticipation(4))
		mesh := &participantTestMesh{nodes: make(map[pid.NodeID]relay.ContextSender)}
		config := ParticipantEndpointConfig{MaxEntries: 16, MaxValueBytes: 16384, MaxWireBytes: 32768, MaxConcurrentRequests: 2, RequestTimeout: 20 * time.Second, RefreshInterval: time.Hour, Cleanup: CleanupConfig{MaxPending: 4, MaxBytes: 4096, BatchSize: 2, RetryInterval: time.Second}}
		endpoint, err := NewParticipantEndpoint(context.Background(), service, mesh, func(context.Context) (pid.NodeID, error) { return "member", nil }, config)
		require.NoError(t, err)
		mesh.nodes["member"] = endpoint
		defer endpoint.Stop(context.Background())
		require.NoError(t, endpoint.Start(context.Background()))
		owner := mkPID("member", "exited")
		_, err = service.Register(context.Background(), "owned", owner)
		require.NoError(t, err)

		sender := &acceptedSnapshotWithoutReply{sent: make(chan struct{})}
		client, err := newParticipantClient(context.Background(), "member", "one", func(context.Context) (pid.NodeID, error) { return "old-authority", nil }, sender, 32768, 16, config.RequestTimeout)
		require.NoError(t, err)
		defer client.stop(context.Background())
		readCtx, cancelRead := context.WithCancel(context.Background())
		defer cancelRead()
		readDone := make(chan error, 1)
		go func() {
			_, err := service.refreshParticipant(readCtx, service.strong.participants, client, participantSnapshotLimits{MaxEntries: 16, MaxValueBytes: 16384})
			readDone <- err
		}()
		<-sender.sent
		event := relay.NewPackage(owner, service.self, topapi.TopicEvents, payload.New(&topapi.ExitEvent{From: owner, Kind: topapi.Exit}))
		require.NoError(t, endpoint.Send(event))
		synctest.Wait()
		stopCtx, cancelStop := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancelStop()
		withdrew := false
		err = endpoint.Retire(stopCtx, func(context.Context) error { withdrew = true; return nil })
		require.NoError(t, err, "cleanup must interrupt stalled read and finish within retirement budget")
		require.ErrorIs(t, <-readDone, context.Canceled)
		require.True(t, withdrew)
		_, err = engine.Get(activeKey("owned"))
		require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	})
}
