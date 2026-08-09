// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/cdc"
)

type stampedTestStream struct {
	changes chan api.Change
	once    sync.Once
}

func (s *stampedTestStream) Changes() <-chan api.Change { return s.changes }

func (s *stampedTestStream) Close() {
	s.once.Do(func() { close(s.changes) })
}

func (*stampedTestStream) Err() error { return nil }

func TestStampedStreamUsesOnlyTheDriverQueue(t *testing.T) {
	upstream := &stampedTestStream{changes: make(chan api.Change, 2)}
	stream := newStampedStream(registry.NewID("app", "cdc"), 7, 65536, upstream)
	require.Zero(t, cap(stream.Changes()), "the common adapter must not add a second event queue")

	upstream.changes <- api.Change{Op: "insert", Table: "users"}
	select {
	case change := <-stream.Changes():
		require.Equal(t, "insert", change.Op)
		require.Equal(t, "users", change.Table)
		require.Equal(t, "app:cdc", change.Source)
		require.Equal(t, registry.NewID("app", "cdc"), change.SourceID)
		require.Equal(t, "7", change.Generation)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stamped change")
	}

	stream.Close()
	select {
	case _, ok := <-stream.Changes():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("stamped stream did not close after upstream close")
	}
}
