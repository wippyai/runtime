// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
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
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed cdc stream")
	}
}
