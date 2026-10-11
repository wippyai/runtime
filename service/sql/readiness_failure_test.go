// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

func TestSQLResourceWaitCancellationKeepsDeleteRetryable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bus := &readinessBus{subs: make(map[string]chan<- event.Event), requests: make(chan event.Event, 1)}
		manager, err := NewManagerWithFactory(&TestTranscoder{}, bus, nil, nil, NewTestPoolFactory())
		require.NoError(t, err)
		id := registry.NewID("test", "db")
		pool := NewMockConnPool(sqlapi.SQLite)
		defer pool.Stop(context.Background())
		manager.services[id] = pool
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() { result <- manager.Delete(ctx, registry.Entry{ID: id}) }()
		synctest.Wait()
		<-bus.requests
		cancel()
		require.ErrorIs(t, <-result, context.Canceled)
		require.Same(t, pool, manager.services[id])
		require.Empty(t, bus.subs)
	})
}

func TestSQLResourceWaitAlreadyCanceledSendsNothing(t *testing.T) {
	bus := &readinessBus{subs: make(map[string]chan<- event.Event), requests: make(chan event.Event, 1)}
	m := &Manager{bus: bus}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, m.awaitResource(ctx, event.Event{System: resource.System, Kind: resource.Delete}), context.Canceled)
	require.Empty(t, bus.requests)
	require.Empty(t, bus.subs)
}
