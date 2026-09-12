// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
)

func TestParticipantExitDispatchBoundedAndJoined(t *testing.T) {
	registry := newStrongReg(t, []string{"node-1"}, 0, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	registry.cleanupSnapshot = func(ctx context.Context) (*participantSnapshot, error) {
		close(entered)
		<-release
		return nil, ctx.Err()
	}
	require.NoError(t, registry.StartReconciler(context.Background()))
	var once sync.Once
	t.Cleanup(func() {
		once.Do(func() { close(release) })
		require.NoError(t, registry.StopReconciler(context.Background()))
	})
	queue := newCleanupQueue(registry, CleanupConfig{MaxPending: 1, MaxBytes: 4096, BatchSize: 1, RetryInterval: time.Hour})
	require.NoError(t, queue.start(registry.reconcileContext()))
	host := &participantHost{registry: registry, cleanup: queue}
	owner := mkPID("node-1", "process")
	event := func() *relay.Package {
		return relay.NewPackage(owner, registry.self, topology.TopicEvents, payload.New(&topology.ExitEvent{From: owner, Kind: topology.Exit}))
	}
	accepted := event()
	lease := &registryReleaseCount{}
	accepted.Messages[0].SetRetentionLease(lease)
	sent := make(chan error, 1)
	go func() { sent <- host.SendContext(context.Background(), accepted) }()
	select {
	case err := <-sent:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("exit dispatch blocked native reader")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not start")
	}
	other := mkPID("node-1", "other")
	extra := relay.NewPackage(other, registry.self, topology.TopicEvents, payload.New(&topology.ExitEvent{From: other, Kind: topology.Exit}))
	require.ErrorIs(t, host.Send(extra), errCleanupCapacity)
	require.True(t, extra.Source.Equal(other))
	relay.ReleasePackage(extra)
	stopped := make(chan error, 1)
	go func() { stopped <- registry.StopReconciler(context.Background()) }()
	select {
	case <-registry.reconcileContext().Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel cleanup owner")
	}
	select {
	case <-stopped:
		t.Fatal("shutdown abandoned accepted cleanup")
	default:
	}
	once.Do(func() { close(release) })
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("shutdown did not join released cleanup")
	}
	require.Equal(t, 1, lease.count)
	require.ErrorIs(t, queue.stop(context.Background()), ErrCleanupIncomplete)
	queue.mu.Lock()
	require.Len(t, queue.jobs, 1, "forced stop must retain unfinished owner identity")
	require.Nil(t, queue.jobs[owner.String()].leave, "forced stop must release mutation admission")
	queue.mu.Unlock()
	late := event()
	require.Error(t, host.Send(late))
	relay.ReleasePackage(late)
}
