// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"sync/atomic"
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

func TestCleanupRetryCompletesBeforeParticipantRetirement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, engine := newParticipantTestInventory(t, 4)
		service := NewService(engine, "member", nil, nil)
		service.ConfigureStrong(StrongDeps{Incarnation: "one", NameGuard: &topapi.NameGuard{}})
		require.NoError(t, service.ConfigureParticipation(4))
		mesh := &participantTestMesh{nodes: make(map[pid.NodeID]relay.ContextSender)}
		config := ParticipantEndpointConfig{MaxEntries: 16, MaxValueBytes: 16384, MaxWireBytes: 32768, MaxConcurrentRequests: 2, RequestTimeout: time.Second, RefreshInterval: time.Hour, Cleanup: CleanupConfig{MaxPending: 4, MaxBytes: 4096, BatchSize: 2, RetryInterval: time.Second}}
		endpoint, err := NewParticipantEndpoint(context.Background(), service, mesh, func(context.Context) (pid.NodeID, error) { return "member", nil }, config)
		require.NoError(t, err)
		mesh.nodes["member"] = endpoint
		defer endpoint.Stop(context.Background())
		require.NoError(t, endpoint.Start(context.Background()))
		owner := mkPID("member", "exited")
		_, err = service.Register(context.Background(), "cleanup-before-retirement", owner)
		require.NoError(t, err)
		capture := service.cleanupSnapshot
		var calls atomic.Int32
		service.cleanupSnapshot = func(ctx context.Context) (*participantSnapshot, error) {
			if calls.Add(1) == 1 {
				return nil, errParticipantUnavailable
			}
			return capture(ctx)
		}
		event := relay.NewPackage(owner, service.self, topapi.TopicEvents, payload.New(&topapi.ExitEvent{From: owner, Kind: topapi.Exit}))
		lease := &registryReleaseCount{}
		event.Messages[0].SetRetentionLease(lease)
		require.NoError(t, endpoint.Send(event))
		require.Equal(t, 1, lease.count, "queue must retain owner identity, not exit payload")
		synctest.Wait()
		require.Equal(t, int32(1), calls.Load())
		retired := make(chan error, 1)
		var withdrew atomic.Bool
		go func() {
			retired <- endpoint.Retire(context.Background(), func(context.Context) error { withdrew.Store(true); return nil })
		}()
		synctest.Wait()
		require.True(t, service.reconciler.Load().mutationsSealed.Load())
		require.False(t, withdrew.Load(), "retirement overtook accepted cleanup")
		select {
		case <-retired:
			t.Fatal("retirement returned with pending cleanup")
		default:
		}
		time.Sleep(config.Cleanup.RetryInterval)
		synctest.Wait()
		require.NoError(t, <-retired)
		require.True(t, withdrew.Load())
		require.Equal(t, int32(2), calls.Load(), "recovery must not need a second incoming exit")
		_, err = engine.Get(activeKey("cleanup-before-retirement"))
		require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
		require.NoError(t, endpoint.Stop(context.Background()))
	})
}

func TestCleanupQueueBoundsDeduplicatesAndReportsUnfinishedStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, engine := newParticipantTestInventory(t, 4)
		service := NewService(engine, "local", nil, nil)
		service.cleanupSnapshot = func(context.Context) (*participantSnapshot, error) { return nil, errParticipantUnavailable }
		owner := mkPID("remote", "a")
		cost := len(owner.String()) + len(owner.Node) + len(owner.Host) + len(owner.UniqID)
		for _, limits := range []CleanupConfig{
			{MaxPending: 1, MaxBytes: 4096, BatchSize: 1, RetryInterval: time.Hour},
			{MaxPending: 4, MaxBytes: cost, BatchSize: 1, RetryInterval: time.Hour},
		} {
			queue := newCleanupQueue(service, limits)
			require.NoError(t, queue.start(context.Background()))
			require.NoError(t, queue.enqueue(context.Background(), owner))
			require.NoError(t, queue.enqueue(context.Background(), owner))
			require.ErrorIs(t, queue.enqueue(context.Background(), mkPID("remote", "b")), errCleanupCapacity)
			synctest.Wait()
			queue.mu.Lock()
			require.Len(t, queue.jobs, 1)
			require.Equal(t, cost, queue.bytes)
			queue.mu.Unlock()
			require.ErrorIs(t, queue.stop(context.Background()), ErrCleanupIncomplete)
			require.ErrorIs(t, queue.stop(context.Background()), ErrCleanupIncomplete, "repeated stop must not turn unfinished cleanup into success")
			queue.mu.Lock()
			require.Len(t, queue.jobs, 1)
			require.Nil(t, queue.jobs[owner.String()].leave)
			queue.mu.Unlock()
		}
	})
}

func TestCleanupIncompleteStopStillFinishesEndpointCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, engine := newParticipantTestInventory(t, 4)
		service := NewService(engine, "member", nil, nil)
		service.ConfigureStrong(StrongDeps{Incarnation: "one", NameGuard: &topapi.NameGuard{}})
		require.NoError(t, service.ConfigureParticipation(4))
		mesh := &participantTestMesh{nodes: make(map[pid.NodeID]relay.ContextSender)}
		endpoint, err := NewParticipantEndpoint(context.Background(), service, mesh, func(context.Context) (pid.NodeID, error) { return "member", nil }, ParticipantEndpointConfig{MaxEntries: 16, MaxValueBytes: 16384, MaxWireBytes: 32768, MaxConcurrentRequests: 2, RequestTimeout: time.Second, RefreshInterval: time.Hour})
		require.NoError(t, err)
		mesh.nodes["member"] = endpoint
		require.NoError(t, endpoint.Start(context.Background()))
		service.cleanupSnapshot = func(context.Context) (*participantSnapshot, error) { return nil, errParticipantUnavailable }
		require.NoError(t, endpoint.host.cleanup.enqueue(context.Background(), mkPID("member", "exited")))
		synctest.Wait()
		require.ErrorIs(t, endpoint.Stop(context.Background()), ErrCleanupIncomplete)
		endpoint.topologyCleanup.Do(func() { t.Error("unfinished cleanup skipped endpoint topology teardown") })
		select {
		case <-endpoint.done:
		default:
			t.Error("stop did not join endpoint operations")
		}
		require.ErrorIs(t, endpoint.Stop(context.Background()), ErrCleanupIncomplete)
	})
}
