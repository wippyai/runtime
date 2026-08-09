// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	config "github.com/wippyai/runtime/api/service/cdc"
)

func TestFilterSet(t *testing.T) {
	assert.Nil(t, filterSet(nil))
	assert.Nil(t, filterSet([]string{"  ", ""}))

	got := filterSet([]string{"Users", " orders ", "users"})
	assert.Contains(t, got, "users")
	assert.Contains(t, got, "orders")
	assert.Len(t, got, 2)
}

func TestSubscriptionMatches(t *testing.T) {
	all := &subscription{}
	assert.True(t, all.matches(config.Change{Op: "insert", Table: "users"}))

	byOp := &subscription{ops: map[string]struct{}{"insert": {}}}
	assert.True(t, byOp.matches(config.Change{Op: "insert"}))
	assert.False(t, byOp.matches(config.Change{Op: "delete"}))

	byTable := &subscription{tables: map[string]struct{}{"users": {}}}
	assert.True(t, byTable.matches(config.Change{Op: "insert", Table: "users"}))
	assert.True(t, byTable.matches(config.Change{Op: "insert", Relation: "users"}))
	assert.False(t, byTable.matches(config.Change{Op: "insert", Table: "orders"}))
}

func TestSubscriptionSnapshotMatchesOnlyTables(t *testing.T) {
	sub := &subscription{
		ops:    map[string]struct{}{"insert": {}},
		tables: map[string]struct{}{"users": {}},
	}

	assert.False(t, sub.matchesSnapshot(config.Change{Op: "snapshot", Table: "orders"}), "table filter still applies to snapshot rows")
	assert.True(t, sub.matchesSnapshot(config.Change{Op: "snapshot", Table: "users"}))
	assert.False(t, sub.matches(config.Change{Op: "delete", Table: "users"}), "op filter still applies to normal changes")
	assert.False(t, sub.matches(config.Change{Op: "insert", Table: "orders"}), "table filter still applies to normal changes")
}

func TestSubscribersPublishAndClose(t *testing.T) {
	subs := newSubscribers()
	stream := subs.subscribe("s", config.StreamOptions{})

	subs.publish(config.Change{Op: "insert", Table: "users", Source: "s"})

	select {
	case change := <-stream.Changes():
		assert.Equal(t, "insert", change.Op)
		assert.Equal(t, "users", change.Table)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for change")
	}

	subs.closeAll()
	select {
	case _, ok := <-stream.Changes():
		assert.False(t, ok, "channel should be closed after closeAll")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for close")
	}
}

func TestSubscribeBufferClamp(t *testing.T) {
	subs := newSubscribers()

	def := subs.subscribe("s", config.StreamOptions{Buffer: 0})
	assert.Equal(t, defaultStreamBuffer, def.maxChanges)

	neg := subs.subscribe("s", config.StreamOptions{Buffer: -5})
	assert.Equal(t, defaultStreamBuffer, neg.maxChanges)

	exact := subs.subscribe("s", config.StreamOptions{Buffer: 7})
	assert.Equal(t, 7, exact.maxChanges)

	huge := subs.subscribe("s", config.StreamOptions{Buffer: maxStreamBuffer + 100})
	assert.Equal(t, maxStreamBuffer, huge.maxChanges)
	assert.Zero(t, cap(def.changes), "the common adapter must not add a second queue")
}

func TestSubscribeAssignsUniqueIncreasingIDs(t *testing.T) {
	subs := newSubscribers()
	a := subs.subscribe("s", config.StreamOptions{})
	b := subs.subscribe("s", config.StreamOptions{})
	assert.Equal(t, uint64(1), a.id)
	assert.Equal(t, uint64(2), b.id)
}

func TestPublishNeverBlocksAndClosesLaggard(t *testing.T) {
	subs := newSubscribers()
	stream := subs.subscribe("s", config.StreamOptions{Buffer: 1})

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			subs.publish(config.Change{Op: "insert", Table: "t"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("publish blocked on a non-reading subscriber")
	}

	for {
		select {
		case _, ok := <-stream.Changes():
			if !ok {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("laggard subscription was not closed on overflow")
		}
	}
}

func TestOverflowedSubscriberDetachesImmediately(t *testing.T) {
	subs := newSubscribers()
	stream := subs.subscribe("s", config.StreamOptions{Buffer: 1})
	change := config.Change{Op: "insert", Table: "users"}

	stream.send(change)
	stream.send(change)

	assert.ErrorIs(t, stream.Err(), errSubscriberOverflow)
	subs.mu.RLock()
	remaining := len(subs.m)
	subs.mu.RUnlock()
	assert.Zero(t, remaining)
}

func TestOverflowedSubscriberChurnDoesNotRetainParentEntries(t *testing.T) {
	subs := newSubscribers()
	change := config.Change{Op: "insert", Table: "users"}
	for i := 0; i < 1000; i++ {
		stream := subs.subscribe("s", config.StreamOptions{Buffer: 1})
		stream.send(change)
		stream.send(change)
	}

	subs.mu.RLock()
	remaining := len(subs.m)
	subs.mu.RUnlock()
	assert.Zero(t, remaining)
}

func TestSubscriptionMaxBytesReleasesOnlyAfterDelivery(t *testing.T) {
	change := config.Change{
		Op:    "insert",
		Table: "users",
		After: map[string]any{"payload": []byte("payload")},
	}
	changeBytes := config.EstimateChangeBytes(change)
	sub := newSubscription("s", config.StreamOptions{Buffer: 2, MaxBytes: changeBytes + 1}, 2)
	defer sub.Close()

	sub.send(change)
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

	sub.send(change)
	assert.NotErrorIs(t, sub.Err(), errSubscriberOverflow)
	select {
	case <-sub.Changes():
	case <-time.After(time.Second):
		t.Fatal("released byte budget did not accept the next change")
	}
}

func TestSubscriptionMaxBytesOverflowIsLocal(t *testing.T) {
	change := config.Change{
		Op:    "insert",
		Table: "users",
		After: map[string]any{"payload": []byte("payload")},
	}
	limit := config.EstimateChangeBytes(change) - 1
	subs := newSubscribers()
	laggard := subs.subscribe("s", config.StreamOptions{MaxBytes: limit})
	reader := subs.subscribe("s", config.StreamOptions{MaxBytes: limit + 1})

	subs.publish(change)
	assert.ErrorIs(t, laggard.Err(), errSubscriberOverflow)
	assert.NotErrorIs(t, reader.Err(), errSubscriberOverflow)
	select {
	case got := <-reader.Changes():
		assert.Equal(t, change.Table, got.Table)
	case <-time.After(time.Second):
		t.Fatal("unrelated subscriber did not receive change")
	}
	reader.Close()
}

func TestSubscribersFilterByOp(t *testing.T) {
	subs := newSubscribers()
	stream := subs.subscribe("s", config.StreamOptions{Ops: []string{"delete"}})
	defer stream.Close()

	subs.publish(config.Change{Op: "insert", Table: "users"})
	subs.publish(config.Change{Op: "delete", Table: "users"})

	select {
	case change := <-stream.Changes():
		require.Equal(t, "delete", change.Op)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}
