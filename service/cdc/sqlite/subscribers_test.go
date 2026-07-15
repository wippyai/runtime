// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"context"
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

func TestSubscribersPublishAndClose(t *testing.T) {
	subs := newSubscribers()
	stream := subs.subscribe("s", config.StreamOptions{}, false)

	subs.publish(context.Background(), config.Change{Op: "insert", Table: "users", Source: "s"})

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

	def := subs.subscribe("s", config.StreamOptions{Buffer: 0}, false)
	assert.Equal(t, defaultStreamBuffer, cap(def.in))

	neg := subs.subscribe("s", config.StreamOptions{Buffer: -5}, false)
	assert.Equal(t, defaultStreamBuffer, cap(neg.in))

	exact := subs.subscribe("s", config.StreamOptions{Buffer: 7}, false)
	assert.Equal(t, 7, cap(exact.in))

	huge := subs.subscribe("s", config.StreamOptions{Buffer: maxStreamBuffer + 100}, false)
	assert.Equal(t, maxStreamBuffer, cap(huge.in))
}

func TestSubscribeAssignsUniqueIncreasingIDs(t *testing.T) {
	subs := newSubscribers()
	a := subs.subscribe("s", config.StreamOptions{}, false)
	b := subs.subscribe("s", config.StreamOptions{}, false)
	assert.Equal(t, uint64(1), a.id)
	assert.Equal(t, uint64(2), b.id)
}

func TestPublishNeverBlocksAndClosesLaggard(t *testing.T) {
	subs := newSubscribers()
	stream := subs.subscribe("s", config.StreamOptions{Buffer: 1}, false)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			subs.publish(context.Background(), config.Change{Op: "insert", Table: "t"})
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

func TestSubscribersFilterByOp(t *testing.T) {
	subs := newSubscribers()
	stream := subs.subscribe("s", config.StreamOptions{Ops: []string{"delete"}}, false)
	defer stream.Close()

	subs.publish(context.Background(), config.Change{Op: "insert", Table: "users"})
	subs.publish(context.Background(), config.Change{Op: "delete", Table: "users"})

	select {
	case change := <-stream.Changes():
		require.Equal(t, "delete", change.Op)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}
