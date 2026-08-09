// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

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
