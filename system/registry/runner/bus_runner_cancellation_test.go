// SPDX-License-Identifier: MPL-2.0

package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

type recordingBus struct {
	mu     sync.Mutex
	events []event.Event
}

func (b *recordingBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (b *recordingBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (b *recordingBus) Unsubscribe(context.Context, event.SubscriberID) {}
func (b *recordingBus) Send(_ context.Context, evt event.Event) {
	b.mu.Lock()
	b.events = append(b.events, evt)
	b.mu.Unlock()
}
func (b *recordingBus) Stop() {}

func (b *recordingBus) kinds() []event.Kind {
	b.mu.Lock()
	defer b.mu.Unlock()
	kinds := make([]event.Kind, len(b.events))
	for i, evt := range b.events {
		kinds[i] = evt.Kind
	}
	return kinds
}

type scriptedWaiter struct {
	result event.AwaitResult
	closes int
}

func (w *scriptedWaiter) Wait() event.AwaitResult { return w.result }
func (w *scriptedWaiter) Close()                  { w.closes++ }

type scriptedAwaitService struct {
	prepare func(context.Context, event.System, event.Kind, event.Path, time.Duration) (event.AwaitWaiter, error)
}

func (s *scriptedAwaitService) Prepare(ctx context.Context, system event.System, kind event.Kind, path event.Path, timeout time.Duration) (event.AwaitWaiter, error) {
	return s.prepare(ctx, system, kind, path, timeout)
}
func (s *scriptedAwaitService) Await(ctx context.Context, system event.System, kind event.Kind, path event.Path, timeout time.Duration) event.AwaitResult {
	waiter, err := s.Prepare(ctx, system, kind, path, timeout)
	if err != nil {
		return event.AwaitResult{Error: err}
	}
	return waiter.Wait()
}
func (*scriptedAwaitService) Start(context.Context) error { return nil }
func (*scriptedAwaitService) Stop() error                 { return nil }

type cancelingBuilder struct {
	*testBuilder
	cancel context.CancelFunc
}

func (b *cancelingBuilder) ApplyOperation(state registry.StateMap, op registry.Operation) (registry.StateMap, error) {
	next, err := b.testBuilder.ApplyOperation(state, op)
	if err == nil && op.Kind == registry.EntryCreate {
		b.cancel()
	}
	return next, err
}

func TestY01TransitionCancellationRollsBack(t *testing.T) {
	root, cancel := context.WithCancel(ctxapi.NewRootContext())
	bus := &recordingBus{}
	var cleanupPrepares int
	await := &scriptedAwaitService{prepare: func(ctx context.Context, _ event.System, kind event.Kind, _ event.Path, _ time.Duration) (event.AwaitWaiter, error) {
		if ctx.Err() == nil {
			if _, ok := ctx.Deadline(); ok {
				cleanupPrepares++
			}
		}
		return &scriptedWaiter{result: event.AwaitResult{Event: event.Event{Kind: kind}, Accepted: true}}, nil
	}}
	ctx := event.WithAwaitService(root, await)
	builder := &cancelingBuilder{testBuilder: newTestBuilder(nil), cancel: cancel}
	runner := NewBusRunner(bus, zap.NewNop(), builder,
		WithTransactionParticipants(func() []string { return []string{"participant"} }),
		WithEventWaitTimeout(time.Second),
	)
	entry := registry.Entry{ID: registry.ParseID("test:item"), Kind: "test", Data: payload.NewString("value")}

	state, err := runner.Transition(ctx, nil, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: entry}})

	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, state)
	require.Equal(t, []event.Kind{registry.TxBegin, registry.EntryCreate, registry.EntryDelete, registry.TxDiscard}, bus.kinds())
	require.Equal(t, 2, cleanupPrepares, "rollback and discard must use the fresh bounded cleanup context")
}

func TestY16WaiterPreparationFailureCleansUp(t *testing.T) {
	first := &scriptedWaiter{}
	prepareErr := errors.New("prepare failed")
	calls := 0
	await := &scriptedAwaitService{prepare: func(context.Context, event.System, event.Kind, event.Path, time.Duration) (event.AwaitWaiter, error) {
		calls++
		if calls == 2 {
			return nil, prepareErr
		}
		return first, nil
	}}
	ctx := event.WithAwaitService(ctxapi.NewRootContext(), await)
	bus := &recordingBus{}
	runner := NewBusRunner(bus, zap.NewNop(), newTestBuilder(nil))

	waiters, err := runner.prepareTransactionWaiters(ctx, []string{"first", "second"}, "registry.tx/1/begin")

	require.ErrorIs(t, err, prepareErr)
	require.Nil(t, waiters)
	require.Equal(t, 1, first.closes)
	require.Empty(t, bus.kinds())
}
