// SPDX-License-Identifier: MPL-2.0

package di

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot"
	contractsys "github.com/wippyai/runtime/system/contract"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/registry/runner"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// Shorten fixed timeouts to exercise delayed acknowledgements without a long
// test delay. Context-bound waits retain their normal semantics.
type contractAwaitBudget struct {
	event.AwaitService
	budgets chan time.Duration
}

func (a *contractAwaitBudget) Prepare(ctx context.Context, system event.System, kind event.Kind, path event.Path, timeout time.Duration) (event.AwaitWaiter, error) {
	if system == contract.System {
		a.budgets <- timeout
		if timeout > 0 {
			timeout = 50 * time.Millisecond
		}
	}
	return a.AwaitService.Prepare(ctx, system, kind, path, timeout)
}

type heldContractAccept struct {
	event.Bus
	ack  chan event.Event
	held atomic.Bool
}

func (b *heldContractAccept) Send(ctx context.Context, evt event.Event) {
	if evt.System == contract.System && evt.Kind == contract.ContractAccept && b.held.CompareAndSwap(false, true) {
		b.ack <- evt
		return
	}
	b.Bus.Send(ctx, evt)
}

type contractTransitionFixture struct {
	ctx     context.Context
	bus     *eventbus.Bus
	manager *Manager
	plane   *contractsys.Registry
	runner  *runner.BusRunner
	held    *heldContractAccept
	budgets chan time.Duration
}

func newContractTransitionFixture(t *testing.T) *contractTransitionFixture {
	t.Helper()
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	ctx := ctxapi.NewRootContext()
	await := eventbus.NewAwaitService(bus)
	require.NoError(t, await.Start(ctx))
	t.Cleanup(func() { _ = await.Stop() })
	budgets := make(chan time.Duration, 8)
	ctx = event.WithAwaitService(ctx, &contractAwaitBudget{AwaitService: await, budgets: budgets})
	ctx = event.WithBus(ctx, bus)
	manager := NewManager(bus, &MockTranscoder{}, zap.NewNop())
	held := &heldContractAccept{Bus: bus, ack: make(chan event.Event, 1)}
	plane := contractsys.NewContractRegistry(held, nil)
	require.NoError(t, plane.Start(ctx))
	t.Cleanup(func() { _ = plane.Stop() })
	handlers := boot.NewHandlerRegistry()
	handlers.RegisterListener("contract.(definition|binding)", manager)
	router, err := eventbus.StartRouter(ctx, bus, eventbus.WithHandlers(handlers.Handlers()...))
	require.NoError(t, err)
	t.Cleanup(func() { _ = router.Stop() })
	br := runner.NewBusRunner(bus, zap.NewNop(), topology.NewStateBuilder(zap.NewNop(), nil),
		runner.WithTransactionParticipants(handlers.TransactionParticipants),
		runner.WithKindHandlerCheck(handlers.HandlesKind))
	return &contractTransitionFixture{ctx: ctx, bus: bus, manager: manager, plane: plane, runner: br, held: held, budgets: budgets}
}

type contractTransitionOutcome struct {
	state registry.State
	err   error
}

func (f *contractTransitionFixture) beginCreate(ctx context.Context, id registry.ID) <-chan contractTransitionOutcome {
	done := make(chan contractTransitionOutcome, 1)
	go func() {
		state, err := f.runner.Transition(ctx, nil, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: definitionEntry(id, "method1")}}, nil)
		done <- contractTransitionOutcome{state, err}
	}()
	return done
}

func (f *contractTransitionFixture) applied(t *testing.T) event.Event {
	t.Helper()
	select {
	case ack := <-f.held.ack:
		return ack
	case <-time.After(3 * time.Second):
		t.Fatal("contract registry did not apply the batch")
		return event.Event{}
	}
}

func TestContractTransitionWaitsForDelayedReply(t *testing.T) {
	f := newContractTransitionFixture(t)
	id := registry.NewID("test", "delayed")
	done := f.beginCreate(f.ctx, id)
	ack := f.applied(t)
	select {
	case result := <-done:
		t.Fatalf("transition returned before its live responder answered: %v", result.err)
	case <-time.After(150 * time.Millisecond):
	}
	f.bus.Send(f.ctx, ack)
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Len(t, result.state, 1)
	case <-time.After(3 * time.Second):
		t.Fatal("transition did not finish after acknowledgement")
	}
	require.Equal(t, event.ContextBoundAwait, <-f.budgets)
	require.Contains(t, f.manager.definitions, id)
	_, err := f.plane.GetContract(f.ctx, id)
	require.NoError(t, err)
}

func TestContractTransitionCancelledCallerRollsBack(t *testing.T) {
	f := newContractTransitionFixture(t)
	id := registry.NewID("test", "cancelled")
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := f.beginCreate(ctx, id)
	ack := f.applied(t)
	cancel()
	select {
	case result := <-done:
		t.Fatalf("transition returned before the in-flight handler and rollback finished: %v", result.err)
	case <-time.After(150 * time.Millisecond):
	}
	f.bus.Send(f.ctx, ack)
	select {
	case result := <-done:
		require.ErrorIs(t, result.err, context.Canceled)
		require.Empty(t, result.state)
	case <-time.After(3 * time.Second):
		t.Fatal("transition did not finish cancellation cleanup")
	}
	require.Empty(t, f.manager.definitions)
	_, err := f.plane.GetContract(f.ctx, id)
	require.Error(t, err)
}

func TestContractTransitionMissingResponder(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	ctx, cancel := context.WithTimeout(ctxapi.NewRootContext(), time.Second)
	defer cancel()
	await := eventbus.NewAwaitService(bus)
	require.NoError(t, await.Start(ctx))
	t.Cleanup(func() { _ = await.Stop() })
	ctx = event.WithAwaitService(ctx, await)
	manager := NewManager(bus, &MockTranscoder{}, zap.NewNop())
	err := manager.Add(ctx, definitionEntry(registry.NewID("test", "missing-responder"), "method1"))
	require.ErrorIs(t, err, ErrContractRegistryNotAvailable)
	require.NoError(t, ctx.Err())
	require.Empty(t, manager.definitions)
}
