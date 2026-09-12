// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/topology/namereg/global"
)

type controlledParticipantFeedSource struct {
	participantSnapshotSource
	calls   atomic.Int32
	fail    atomic.Bool
	entered chan struct{}
	gate    chan struct{}
}

func (s *controlledParticipantFeedSource) ScanAtIndex(prefix string, fn func(kvapi.Entry) bool) (uint64, error) {
	if s.calls.Add(1) > 1 && s.gate != nil {
		select {
		case s.entered <- struct{}{}:
		default:
		}
		<-s.gate
	}
	if s.fail.Load() {
		return 0, errors.New("authority unavailable")
	}
	return s.participantSnapshotSource.ScanAtIndex(prefix, fn)
}

func TestParticipantFeedCoalescesHintsAndJoinsBlockedRefresh(t *testing.T) {
	inventory, authority := newParticipantTestInventory(t, 4)
	_, local := newParticipantTestInventory(t, 4)
	client := NewService(local, "client", nil, nil)
	client.ConfigureDissem(global.NewDissem("client", nil))
	client.SetNonMember(func() bool { return true })
	client.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
	source := &controlledParticipantFeedSource{participantSnapshotSource: authority, entered: make(chan struct{}, 1), gate: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
	require.NoError(t, client.startParticipantFeed(ctx, inventory, source, limits, time.Hour))
	require.True(t, client.NameReady())
	client.requestParticipantRefresh()
	select {
	case <-source.entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not begin")
	}
	for range 10000 {
		client.requestParticipantRefresh()
	}
	require.Len(t, client.reconciler.Load().refresh, 1)
	require.EqualValues(t, 2, source.calls.Load(), "hints must not spawn concurrent captures")
	deadline, done := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer done()
	require.ErrorIs(t, client.StopReconciler(deadline), context.DeadlineExceeded)
	require.False(t, client.NameReady())
	close(source.gate)
	joined, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, client.StopReconciler(joined))
	require.EqualValues(t, 2, source.calls.Load(), "shutdown must not execute queued refreshes")
	require.Error(t, client.startParticipantFeed(ctx, inventory, source, limits, time.Hour), "a stopped service must not reuse enrollment ownership")
}

func TestParticipantFeedBackstopRecoversAfterAuthorityError(t *testing.T) {
	inventory, authority := newParticipantTestInventory(t, 4)
	_, local := newParticipantTestInventory(t, 4)
	client := NewService(local, "client", nil, nil)
	client.SetNonMember(func() bool { return true })
	client.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
	source := &controlledParticipantFeedSource{participantSnapshotSource: authority}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, client.startParticipantFeed(ctx, inventory, source, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}, 20*time.Millisecond))
	t.Cleanup(func() { cancel(); require.NoError(t, client.StopReconciler(context.Background())) })
	source.fail.Store(true)
	client.requestParticipantRefresh()
	require.Eventually(t, func() bool { return !client.NameReady() }, time.Second, time.Millisecond)
	source.fail.Store(false)
	// No hint: configured periodic refresh must repair a lost notification.
	require.Eventually(t, client.NameReady, time.Second, time.Millisecond)
	require.GreaterOrEqual(t, source.calls.Load(), int32(3))
}

type contextParticipantFeedSource struct {
	participantSnapshotSource
	calls   atomic.Int32
	entered chan struct{}
}

func (s *contextParticipantFeedSource) ScanAtIndexContext(ctx context.Context, prefix string, visit func(kvapi.Entry) bool) (uint64, error) {
	if s.calls.Add(1) == 1 {
		return s.participantSnapshotSource.ScanAtIndex(prefix, visit)
	}
	close(s.entered)
	<-ctx.Done()
	return 0, ctx.Err()
}

func TestParticipantFeedCancelsContextAwareAuthorityWait(t *testing.T) {
	inventory, authority := newParticipantTestInventory(t, 4)
	_, replica := newParticipantTestInventory(t, 4)
	client := NewService(replica, "client", nil, nil)
	client.SetNonMember(func() bool { return true })
	client.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
	source := &contextParticipantFeedSource{participantSnapshotSource: authority, entered: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, client.startParticipantFeed(ctx, inventory, source, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}, time.Hour))
	client.requestParticipantRefresh()
	select {
	case <-source.entered:
	case <-time.After(time.Second):
		t.Fatal("authority wait did not start")
	}
	stop, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	require.NoError(t, client.StopReconciler(stop), "feed cancellation must interrupt a context-aware request and join it")
	require.False(t, client.NameReady())
}

func TestParticipantFeedFailedStartRetryRestoresTimerOwnership(t *testing.T) {
	inventory, authority := newParticipantTestInventory(t, 4)
	_, local := newParticipantTestInventory(t, 4)
	client := NewService(local, "client", nil, nil)
	client.SetNonMember(func() bool { return true })
	client.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
	source := &controlledParticipantFeedSource{participantSnapshotSource: authority}
	source.fail.Store(true)
	ctx := context.Background()
	limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
	require.Error(t, client.startParticipantFeed(ctx, inventory, source, limits, time.Hour))
	require.False(t, client.NameReady())
	require.Nil(t, client.reconciler.Load())
	source.fail.Store(false)
	require.NoError(t, client.startParticipantFeed(ctx, inventory, source, limits, time.Hour))
	t.Cleanup(func() { require.NoError(t, client.StopReconciler(ctx)) })
	client.strong.armTimer("pending", time.Now().Add(time.Hour).UnixNano())
	client.strong.mu.Lock()
	timer := client.strong.timers["pending"]
	client.strong.mu.Unlock()
	require.NotNil(t, timer, "failed bootstrap must not permanently seal the retried lifetime's timers")
	require.NoError(t, client.StopReconciler(ctx))
	select {
	case <-timer.done:
	default:
		t.Fatal("stop did not join timer ownership")
	}
	client.strong.armTimer("after-stop", time.Now().Add(time.Hour).UnixNano())
	client.strong.mu.Lock()
	after := client.strong.timers["after-stop"]
	client.strong.mu.Unlock()
	require.Nil(t, after, "successful stop remains terminal")
}

type preemptedParticipantFeedSource struct {
	participantSnapshotSource
	calls   atomic.Int32
	entered chan struct{}
}

func (s *preemptedParticipantFeedSource) ScanAtIndexContext(ctx context.Context, prefix string, visit func(kvapi.Entry) bool) (uint64, error) {
	if s.calls.Add(1) == 2 {
		close(s.entered)
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return s.participantSnapshotSource.ScanAtIndex(prefix, visit)
}

func TestParticipantFeedResumesAfterCleanupWithoutBackstopWait(t *testing.T) {
	inventory, authority := newParticipantTestInventory(t, 4)
	_, local := newParticipantTestInventory(t, 4)
	client := NewService(local, "client", nil, nil)
	client.SetNonMember(func() bool { return true })
	client.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
	source := &preemptedParticipantFeedSource{participantSnapshotSource: authority, entered: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, client.startParticipantFeed(ctx, inventory, source, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}, time.Hour))
	t.Cleanup(func() { cancel(); require.NoError(t, client.StopReconciler(context.Background())) })
	client.requestParticipantRefresh()
	select {
	case <-source.entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not begin")
	}
	finishFirst := client.prioritizeParticipantCleanup()
	finishSecond := client.prioritizeParticipantCleanup()
	require.Eventually(t, func() bool { return !client.NameReady() }, time.Second, time.Millisecond)
	finishFirst()
	require.Empty(t, client.reconciler.Load().refresh, "another cleanup still owns priority")
	finishSecond()
	require.Eventually(t, client.NameReady, time.Second, time.Millisecond, "must not wait for hour-long backstop")
	require.GreaterOrEqual(t, source.calls.Load(), int32(3))
}
