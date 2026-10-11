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
	"github.com/wippyai/runtime/system/eventbus"
)

func TestSQLResourceWaitCancellationAfterDispatchStillReportsRejection(t *testing.T) {
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
		request := <-bus.requests
		cancel()
		synctest.Wait()
		select {
		case err := <-result:
			t.Fatalf("abandoned an in-flight resource operation: %v", err)
		default:
		}
		bus.reply(event.Event{System: resource.System, Kind: resource.Reject, Path: request.Path, Data: "routing unchanged"})
		require.ErrorContains(t, <-result, "routing unchanged")
		require.Same(t, pool, manager.services[id])
		require.Empty(t, bus.subs)
	})
}

type cancelOnSubscribeBus struct {
	*readinessBus
	cancel context.CancelFunc
}

func (b *cancelOnSubscribeBus) SubscribeP(ctx context.Context, system, pattern string, ch chan<- event.Event) (string, error) {
	id, err := b.readinessBus.SubscribeP(ctx, system, pattern, ch)
	b.cancel()
	return id, err
}

func TestSQLResourceCancellationBeforeDispatchSendsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := &cancelOnSubscribeBus{readinessBus: &readinessBus{subs: make(map[string]chan<- event.Event), requests: make(chan event.Event, 1)}, cancel: cancel}
	m := &Manager{bus: bus}
	require.ErrorIs(t, m.awaitResource(ctx, event.Event{System: resource.System, Kind: resource.Delete}), context.Canceled)
	require.Empty(t, bus.requests)
	require.Empty(t, bus.subs)
}

type shutdownReadinessBus struct {
	*readinessBus
	done chan struct{}
}

func (b *shutdownReadinessBus) Done() <-chan struct{} { return b.done }

func TestSQLResourceHandoffEndsOnBusShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bus := &shutdownReadinessBus{readinessBus: &readinessBus{subs: make(map[string]chan<- event.Event), requests: make(chan event.Event, 1)}, done: make(chan struct{})}
		m := &Manager{bus: bus}
		done := make(chan error, 1)
		go func() {
			done <- m.awaitResource(context.Background(), event.Event{System: resource.System, Kind: resource.Delete})
		}()
		<-bus.requests
		close(bus.done)
		require.ErrorIs(t, <-done, eventbus.ErrBusClosed)
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
