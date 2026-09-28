// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
)

func TestSourceSubscribePublishesMatchingChanges(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	stream := src.Subscribe(cdcapi.StreamOptions{
		Tables: []string{"public.accounts"},
		Ops:    []string{"insert"},
		Buffer: 2,
	})
	defer stream.Close()

	src.publishChange(context.Background(), cdcapi.Change{
		Source:   "test:cdc",
		Op:       "insert",
		Schema:   "public",
		Table:    "accounts",
		Relation: "public.accounts",
		After:    map[string]any{"id": int64(1)},
	})

	select {
	case got := <-stream.Changes():
		assert.Equal(t, "insert", got.Op)
		assert.Equal(t, "public.accounts", got.Relation)
		assert.Equal(t, int64(1), got.After["id"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cdc change")
	}
}

func TestSourceSubscribeAllowsOrdinaryPreStartStream(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	stream, err := src.subscribe(context.Background(), cdcapi.StreamOptions{Buffer: 1})
	require.NoError(t, err)
	require.NotNil(t, stream)
	defer stream.Close()

	src.publishChange(context.Background(), cdcapi.Change{
		Op:     "insert",
		Table:  "accounts",
		After:  map[string]any{"id": int64(1)},
		Source: "test:cdc",
	})

	select {
	case got := <-stream.Changes():
		require.Equal(t, "insert", got.Op)
		require.Equal(t, "accounts", got.Table)
	case <-time.After(time.Second):
		t.Fatal("pre-start subscription did not retain the ordinary stream")
	}
}

func TestSourceSnapshotDefaultRequiresRunningGeneration(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a", Snapshot: true})
	stream, err := src.subscribe(context.Background(), cdcapi.StreamOptions{})
	assert.ErrorIs(t, err, cdcapi.ErrSourceNotReady)
	assert.Nil(t, stream)
	assert.Nil(t, src.Subscribe(cdcapi.StreamOptions{}))
}

func TestSnapshotHandoffWaitsForReplicationFence(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	done := make(chan struct{})
	src.mu.Lock()
	src.done = done
	src.streamPosition = 0
	src.mu.Unlock()

	fence, err := pglogrepl.ParseLSN("0/20")
	require.NoError(t, err)
	waited := make(chan error, 1)
	go func() { waited <- src.waitStreamPosition(context.Background(), fence) }()
	select {
	case err := <-waited:
		t.Fatalf("handoff released before replication reached fence: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	src.advanceStreamPosition(done, fence)
	select {
	case err := <-waited:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("handoff did not observe the replication fence")
	}
}

func TestIdleKeepaliveAdvancesSnapshotWatermark(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	done := make(chan struct{})
	src.mu.Lock()
	src.done = done
	src.streamPosition = 0
	src.mu.Unlock()

	fence, err := pglogrepl.ParseLSN("0/40")
	require.NoError(t, err)
	waited := make(chan error, 1)
	go func() { waited <- src.waitStreamPosition(context.Background(), fence) }()
	select {
	case err := <-waited:
		t.Fatalf("idle snapshot released before keepalive: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	src.observeKeepalive(done, pglogrepl.PrimaryKeepaliveMessage{ServerWALEnd: fence})
	select {
	case err := <-waited:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("idle keepalive did not advance snapshot watermark")
	}
}

func TestSnapshotGateSerializesPerSourceAndIsolatesSources(t *testing.T) {
	first := NewSource(SourceOptions{Name: "db-one", Slot: "slot_one"})
	second := NewSource(SourceOptions{Name: "db-two", Slot: "slot_two"})
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, first.acquireSnapshot(ctx))

	waiting := make(chan error, 1)
	go func() { waiting <- first.acquireSnapshot(ctx) }()
	select {
	case err := <-waiting:
		t.Fatalf("same-source snapshot gate was not serialized: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	require.NoError(t, second.acquireSnapshot(context.Background()))
	second.releaseSnapshot()
	cancel()
	select {
	case err := <-waiting:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cancelled snapshot did not leave the per-source gate")
	}
	first.releaseSnapshot()
}

func TestSubscriptionCloseWaitsForSnapshotWorker(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	sub := src.newSubscription(cdcapi.StreamOptions{Snapshot: true})
	cancelled := make(chan struct{})
	workerDone := make(chan struct{})
	require.True(t, sub.registerSnapshot(func() { close(cancelled) }, workerDone))
	go func() {
		<-cancelled
		sub.finishSnapshotWorker()
	}()

	sub.Close()
	select {
	case <-workerDone:
	default:
		t.Fatal("subscription Close returned before its snapshot worker joined")
	}
}

func TestStopJoinsSnapshotWorkerBeforeGenerationReset(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	sub := src.newSubscription(cdcapi.StreamOptions{Snapshot: true})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	workerDone := make(chan struct{})
	require.True(t, sub.registerSnapshot(func() { close(cancelled) }, workerDone))
	src.snapshotWG.Add(1)
	go func() {
		<-cancelled
		<-release
		sub.finishSnapshotWorker()
		src.snapshotWG.Done()
	}()

	stopped := make(chan error, 1)
	go func() { stopped <- src.Stop(context.Background()) }()
	select {
	case err := <-stopped:
		t.Fatalf("Stop returned before snapshot worker joined: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	src.mu.Lock()
	assert.Equal(t, sourceStopping, src.state)
	src.mu.Unlock()
	close(release)
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Stop did not complete after snapshot worker release")
	}
	src.mu.Lock()
	assert.Equal(t, sourceStopped, src.state)
	src.mu.Unlock()
}

func TestOldGenerationCleanupDoesNotCloseReplacementSubscribers(t *testing.T) {
	src := NewSource(SourceOptions{Name: "db-one", Slot: "slot_one"})
	oldDone := make(chan struct{})
	src.mu.Lock()
	src.state = sourceRunning
	src.done = oldDone
	src.mu.Unlock()
	oldSub := src.Subscribe(cdcapi.StreamOptions{Buffer: 2})
	require.NotNil(t, oldSub)

	current, detached := src.finishRunGeneration(oldDone)
	require.True(t, current)
	src.closeDetachedSubscriptions(detached, nil)
	close(oldDone)

	newDone := make(chan struct{})
	src.mu.Lock()
	src.state = sourceRunning
	src.done = newDone
	src.mu.Unlock()
	newSub := src.Subscribe(cdcapi.StreamOptions{Buffer: 2})
	require.NotNil(t, newSub)
	defer newSub.Close()

	select {
	case _, ok := <-oldSub.Changes():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("old generation subscriber was not closed")
	}
	src.publishChange(context.Background(), cdcapi.Change{Op: "insert", Table: "users"})
	select {
	case change := <-newSub.Changes():
		require.Equal(t, "insert", change.Op)
	case <-time.After(time.Second):
		t.Fatal("replacement subscriber was closed by old generation cleanup")
	}

	other := NewSource(SourceOptions{Name: "db-two", Slot: "slot_two"})
	otherDone := make(chan struct{})
	other.mu.Lock()
	other.state = sourceRunning
	other.done = otherDone
	other.mu.Unlock()
	otherSub := other.Subscribe(cdcapi.StreamOptions{Buffer: 2})
	require.NotNil(t, otherSub)
	defer otherSub.Close()
	other.publishChange(context.Background(), cdcapi.Change{Op: "insert", Table: "isolated"})
	select {
	case change := <-otherSub.Changes():
		require.Equal(t, "isolated", change.Table)
	case <-time.After(time.Second):
		t.Fatal("independent source subscriber was affected by generation cleanup")
	}
}

func TestSourceSubscribeFiltersChanges(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	stream := src.Subscribe(cdcapi.StreamOptions{
		Tables: []string{"public.accounts"},
		Ops:    []string{"update"},
		Buffer: 2,
	})
	defer stream.Close()

	src.publishChange(context.Background(), cdcapi.Change{Op: "insert", Table: "accounts", Relation: "public.accounts"})
	src.publishChange(context.Background(), cdcapi.Change{Op: "update", Table: "orders", Relation: "public.orders"})

	select {
	case got := <-stream.Changes():
		t.Fatalf("unexpected change delivered: %#v", got)
	case <-time.After(25 * time.Millisecond):
	}
}

func TestSourceSubscriptionCloseReleasesChannel(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	stream := src.Subscribe(cdcapi.StreamOptions{})

	src.subMu.RLock()
	require.Len(t, src.subs, 1)
	src.subMu.RUnlock()

	stream.Close()

	src.subMu.RLock()
	assert.Empty(t, src.subs)
	src.subMu.RUnlock()

	select {
	case _, ok := <-stream.Changes():
		assert.False(t, ok)
	default:
		t.Fatal("stream channel was not closed synchronously")
	}
	assert.NoError(t, stream.Err())
}

func TestSourceSubscriptionRetainsTerminalError(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	stream := src.Subscribe(cdcapi.StreamOptions{})
	err := errors.New("replication failed")
	src.closeSubscriptionsWithError(err)

	select {
	case _, ok := <-stream.Changes():
		assert.False(t, ok)
	default:
		t.Fatal("stream channel was not closed synchronously")
	}
	assert.ErrorIs(t, stream.(interface{ Err() error }).Err(), err)
}

func TestSourceSubscriptionChurnDetachesImmediately(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	for i := 0; i < 1000; i++ {
		stream := src.Subscribe(cdcapi.StreamOptions{Buffer: 1})
		stream.Close()
		select {
		case _, ok := <-stream.Changes():
			require.False(t, ok)
		default:
			t.Fatal("churned stream channel was not closed")
		}
	}
	src.subMu.RLock()
	defer src.subMu.RUnlock()
	assert.Empty(t, src.subs)
}

func TestSourceSubscriptionIsPrunedWhenStopWins(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	src.mu.Lock()
	src.state = sourceRunning
	src.mu.Unlock()

	stream, err := src.subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)
	require.NotNil(t, stream)

	require.NoError(t, src.Stop(context.Background()))
	assert.Eventually(t, func() bool {
		src.subMu.RLock()
		defer src.subMu.RUnlock()
		return len(src.subs) == 0
	}, time.Second, time.Millisecond)

	select {
	case _, ok := <-stream.Changes():
		assert.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("stopped source left a live subscription")
	}
	_, err = src.subscribe(context.Background(), cdcapi.StreamOptions{})
	assert.ErrorIs(t, err, cdcapi.ErrSourceNotReady)
}

func TestSourceSubscriptionOverflowIsBoundedAndLocal(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	laggard := src.Subscribe(cdcapi.StreamOptions{Buffer: 1})
	reader := src.Subscribe(cdcapi.StreamOptions{Buffer: maxStreamBuffer})

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			src.publishChange(context.Background(), cdcapi.Change{
				Op:       "insert",
				Table:    "accounts",
				Relation: "public.accounts",
			})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("replication fan-out blocked on a slow subscriber")
	}

	assert.Eventually(t, func() bool {
		return errors.Is(laggard.(interface{ Err() error }).Err(), errSubscriberOverflow)
	}, time.Second, time.Millisecond, "laggard must terminate with an overflow error")
	select {
	case _, ok := <-reader.Changes():
		assert.True(t, ok, "an unrelated subscriber must remain active")
	case <-time.After(time.Second):
		t.Fatal("unrelated subscriber did not receive a change")
	}
	laggard.Close()
	reader.Close()
}

func TestSourceSubscriptionMaxBytesReleasesOnlyAfterDelivery(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	change := cdcapi.Change{
		Op:    "insert",
		Table: "users",
		After: map[string]any{"payload": []byte("payload")},
	}
	changeBytes := cdcapi.EstimateChangeBytes(change)
	stream := src.newSubscription(cdcapi.StreamOptions{Buffer: 2, MaxBytes: changeBytes + 1})
	sub := stream
	defer sub.Close()

	sub.send(context.Background(), change, cdcapi.EstimateChangeBytes(change))
	assert.Eventually(t, func() bool {
		sub.mu.Lock()
		defer sub.mu.Unlock()
		return len(sub.queue) == 1 && sub.queuedBytes == changeBytes
	}, time.Second, time.Millisecond)

	select {
	case got := <-sub.Changes():
		assert.Equal(t, change.Table, got.Table)
	case <-time.After(time.Second):
		t.Fatal("timed out receiving queued change")
	}
	assert.Eventually(t, func() bool {
		sub.mu.Lock()
		defer sub.mu.Unlock()
		return len(sub.queue) == 0 && sub.queuedBytes == 0
	}, time.Second, time.Millisecond)

	sub.send(context.Background(), change, cdcapi.EstimateChangeBytes(change))
	assert.NotErrorIs(t, sub.Err(), errSubscriberOverflow)
	select {
	case <-sub.Changes():
	case <-time.After(time.Second):
		t.Fatal("released byte budget did not accept the next change")
	}
}

func TestSourceSubscriptionMaxBytesOverflowIsIsolated(t *testing.T) {
	change := cdcapi.Change{
		Op:    "insert",
		Table: "users",
		After: map[string]any{"payload": []byte("payload")},
	}
	limit := cdcapi.EstimateChangeBytes(change) - 1
	first := NewSource(SourceOptions{Name: "test:first", Slot: "slot_first"})
	second := NewSource(SourceOptions{Name: "test:second", Slot: "slot_second"})
	firstStream := first.Subscribe(cdcapi.StreamOptions{MaxBytes: limit})
	secondStream := second.Subscribe(cdcapi.StreamOptions{MaxBytes: limit + 1})
	defer firstStream.Close()
	defer secondStream.Close()

	first.publishChange(context.Background(), change)
	assert.ErrorIs(t, firstStream.(interface{ Err() error }).Err(), errSubscriberOverflow)
	second.publishChange(context.Background(), change)
	assert.NotErrorIs(t, secondStream.(interface{ Err() error }).Err(), errSubscriberOverflow)
	select {
	case got := <-secondStream.Changes():
		assert.Equal(t, change.Table, got.Table)
	case <-time.After(time.Second):
		t.Fatal("independent source did not receive change")
	}
}

func TestSnapshotSubscriptionHandoffIsCommitLSNFenced(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	sub := src.newSubscription(cdcapi.StreamOptions{Snapshot: true, Buffer: 8})
	defer sub.Close()

	before := cdcapi.Change{Op: "insert", Table: "users", CommitLSN: "0/10"}
	after := cdcapi.Change{Op: "insert", Table: "users", CommitLSN: "0/30"}
	sub.send(context.Background(), before, cdcapi.EstimateChangeBytes(before))
	sub.send(context.Background(), after, cdcapi.EstimateChangeBytes(after))
	select {
	case got := <-sub.Changes():
		t.Fatalf("live change escaped before snapshot completion: %#v", got)
	case <-time.After(20 * time.Millisecond):
	}

	fence, err := pglogrepl.ParseLSN("0/20")
	require.NoError(t, err)
	snapshot := cdcapi.Change{
		Op:        "snapshot",
		Table:     "users",
		CommitLSN: fence.String(),
		After:     map[string]any{"id": int64(1)},
	}
	require.NoError(t, sub.sendSnapshot(snapshot, cdcapi.EstimateChangeBytes(snapshot)))
	sub.finishSnapshot(fence, nil)

	select {
	case got := <-sub.Changes():
		require.Equal(t, "snapshot", got.Op)
	case <-time.After(time.Second):
		t.Fatal("snapshot row was not delivered")
	}
	select {
	case got := <-sub.Changes():
		require.Equal(t, after.CommitLSN, got.CommitLSN)
	case <-time.After(time.Second):
		t.Fatal("post-fence live row was not delivered")
	}
}

func TestSnapshotSubscriptionBoundsPendingLiveChanges(t *testing.T) {
	src := NewSource(SourceOptions{Name: "test:cdc", Slot: "slot_a"})
	change := cdcapi.Change{
		Op:    "insert",
		Table: "users",
		After: map[string]any{"payload": []byte("large")},
	}
	bytes := cdcapi.EstimateChangeBytes(change)
	sub := src.newSubscription(cdcapi.StreamOptions{Snapshot: true, MaxBytes: bytes})
	defer sub.Close()
	sub.send(context.Background(), change, bytes)
	sub.send(context.Background(), change, bytes)
	assert.ErrorIs(t, sub.Err(), errSubscriberOverflow)
	select {
	case _, ok := <-sub.Changes():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("overflowed snapshot stream did not close")
	}
}

func TestSnapshotSubscriptionsDoNotSharePendingState(t *testing.T) {
	firstSource := NewSource(SourceOptions{Name: "db-one", Slot: "slot_one"})
	secondSource := NewSource(SourceOptions{Name: "db-two", Slot: "slot_two"})
	first := firstSource.newSubscription(cdcapi.StreamOptions{Snapshot: true, Buffer: 4})
	second := secondSource.newSubscription(cdcapi.StreamOptions{Snapshot: true, Buffer: 4})
	defer first.Close()
	defer second.Close()

	firstChange := cdcapi.Change{Op: "insert", Table: "first", CommitLSN: "0/30"}
	secondChange := cdcapi.Change{Op: "insert", Table: "second", CommitLSN: "0/30"}
	first.send(context.Background(), firstChange, cdcapi.EstimateChangeBytes(firstChange))
	second.send(context.Background(), secondChange, cdcapi.EstimateChangeBytes(secondChange))
	fence, err := pglogrepl.ParseLSN("0/20")
	require.NoError(t, err)
	first.finishSnapshot(fence, nil)
	second.finishSnapshot(fence, nil)

	select {
	case got := <-first.Changes():
		require.Equal(t, "first", got.Table)
	case <-time.After(time.Second):
		t.Fatal("first source did not deliver its pending change")
	}
	select {
	case got := <-second.Changes():
		require.Equal(t, "second", got.Table)
	case <-time.After(time.Second):
		t.Fatal("second source did not deliver its pending change")
	}
}
