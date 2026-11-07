// SPDX-License-Identifier: MPL-2.0

package di

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/contract"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	apidi "github.com/wippyai/runtime/api/service/di"
	contractsys "github.com/wippyai/runtime/system/contract"
	"github.com/wippyai/runtime/system/eventbus"
)

// awaitContext attaches a started AwaitService for bus to a root context, the
// way boot hands one to registry listeners.
func awaitContext(t *testing.T, bus event.Bus) context.Context {
	t.Helper()
	await := eventbus.NewAwaitService(bus)
	require.NoError(t, await.Start(ctxapi.NewRootContext()))
	t.Cleanup(func() { _ = await.Stop() })
	return event.WithAwaitService(ctxapi.NewRootContext(), await)
}

// gatedPlane answers change batches only when the test tells it to, so a test
// can observe what the manager does before and after the contract registry
// replies.
type gatedPlane struct {
	bus      event.Bus
	received chan event.Event
}

func newGatedPlane(t *testing.T, bus event.Bus) *gatedPlane {
	t.Helper()
	plane := &gatedPlane{bus: bus, received: make(chan event.Event, 16)}
	sub, err := eventbus.NewSubscriber(ctxapi.NewRootContext(), bus, contract.System, contract.ApplyChanges,
		func(evt event.Event) { plane.received <- evt })
	require.NoError(t, err)
	t.Cleanup(sub.Close)
	return plane
}

func (p *gatedPlane) next(t *testing.T) event.Event {
	t.Helper()
	select {
	case evt := <-p.received:
		return evt
	case <-time.After(5 * time.Second):
		t.Fatal("contract plane received no change batch")
		return event.Event{}
	}
}

func (p *gatedPlane) reply(evt event.Event, kind event.Kind, data any) {
	p.bus.Send(ctxapi.NewRootContext(), event.Event{System: contract.System, Kind: kind, Path: evt.Path, Data: data})
}

func (p *gatedPlane) accept(t *testing.T) event.Event {
	t.Helper()
	evt := p.next(t)
	p.reply(evt, contract.ContractAccept, nil)
	return evt
}

func requirePending(t *testing.T, done <-chan error, what string) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("%s returned (%v) before the contract plane acknowledged the change", what, err)
	case <-time.After(100 * time.Millisecond):
	}
}

func requireReturned(t *testing.T, done <-chan error, what string) {
	t.Helper()
	select {
	case err := <-done:
		require.NoError(t, err, what)
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not return after the contract plane acknowledged the change", what)
	}
}

// A committed contract change is in effect when the registry operation
// returns: Commit sends the whole transaction as one change batch and
// completes only once the contract registry has accepted it.
func TestManager_CommitReturnsAfterContractPlaneAcknowledges(t *testing.T) {
	manager, bus := newDIManager()
	ctx := awaitContext(t, bus)
	plane := newGatedPlane(t, bus)
	defID := registry.NewID("test", "contract")
	bindingID := registry.NewID("test", "binding")

	require.NoError(t, manager.Begin(ctx))
	require.NoError(t, manager.Add(ctx, definitionEntry(defID, "method1")))
	require.NoError(t, manager.Add(ctx, bindingEntry(bindingID, defID, "method1")))

	done := make(chan error, 1)
	go func() { done <- manager.Commit(ctx) }()

	batch := plane.next(t)
	changes, ok := batch.Data.(contract.Changes)
	require.True(t, ok, "Commit must send one change batch, got %T", batch.Data)
	require.Len(t, changes, 2)
	assert.Equal(t, contract.RegisterDefinition, changes[0].Kind)
	assert.Equal(t, contract.RegisterBinding, changes[1].Kind)

	requirePending(t, done, "Commit")
	plane.reply(batch, contract.ContractAccept, nil)
	requireReturned(t, done, "Commit")
}

// A refused batch changes nothing: Commit fails, the manager keeps its
// committed state, and the rollback the registry runner dispatches before
// Discard leaves the manager usable for the same entries.
func TestManager_RefusedCommitLeavesContractsUnchanged(t *testing.T) {
	manager, bus := newDIManager()
	ctx := awaitContext(t, bus)
	plane := newGatedPlane(t, bus)
	defID := registry.NewID("test", "contract")
	bindingID := registry.NewID("test", "binding")

	require.NoError(t, manager.Begin(ctx))
	require.NoError(t, manager.Add(ctx, definitionEntry(defID, "method1")))
	require.NoError(t, manager.Add(ctx, bindingEntry(bindingID, defID, "method1")))

	done := make(chan error, 1)
	go func() { done <- manager.Commit(ctx) }()
	plane.reply(plane.next(t), contract.ContractReject, "invalid binding payload")
	select {
	case err := <-done:
		requireAPIError(t, err, apierror.Unavailable, "contract registry did not apply change batch")
		assert.Contains(t, err.Error(), "invalid binding payload")
	case <-time.After(5 * time.Second):
		t.Fatal("Commit did not return after the batch was refused")
	}
	assert.Empty(t, manager.definitions)
	assert.Empty(t, manager.bindings)

	require.NoError(t, manager.Delete(ctx, registry.Entry{ID: bindingID, Kind: apidi.Binding}))
	require.NoError(t, manager.Delete(ctx, registry.Entry{ID: defID, Kind: apidi.Definition}))
	require.NoError(t, manager.Discard(ctx))

	go func() { done <- manager.Add(ctx, definitionEntry(defID, "method1")) }()
	plane.accept(t)
	requireReturned(t, done, "Add after a refused Commit")
}

// A reply belongs to the batch that caused it: a late acceptance of an
// abandoned request does not complete a newer operation on the same entry.
func TestManager_StaleAcknowledgementDoesNotCompleteNewerOperation(t *testing.T) {
	manager, bus := newDIManager()
	ctx := awaitContext(t, bus)
	plane := newGatedPlane(t, bus)
	defID := registry.NewID("test", "contract")

	abandoned, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	require.Error(t, manager.Add(abandoned, definitionEntry(defID, "method1")))
	stale := plane.next(t)

	done := make(chan error, 1)
	go func() { done <- manager.Add(ctx, definitionEntry(defID, "method1")) }()
	current := plane.next(t)

	plane.reply(stale, contract.ContractAccept, nil)
	requirePending(t, done, "Add")
	plane.reply(current, contract.ContractAccept, nil)
	requireReturned(t, done, "Add")
}

// Outside a transaction each operation is in effect when it returns.
func TestManager_OperationReturnsAfterContractPlaneAcknowledges(t *testing.T) {
	manager, bus := newDIManager()
	ctx := awaitContext(t, bus)
	plane := newGatedPlane(t, bus)
	defID := registry.NewID("test", "contract")

	done := make(chan error, 1)
	go func() { done <- manager.Add(ctx, definitionEntry(defID, "method1")) }()
	requirePending(t, done, "Add")
	plane.accept(t)
	requireReturned(t, done, "Add")

	go func() { done <- manager.Delete(ctx, definitionEntry(defID)) }()
	requirePending(t, done, "Delete")
	plane.accept(t)
	requireReturned(t, done, "Delete")
}

// A definition deleted and re-created through registry transactions resolves
// through the contract registry as soon as each Commit returns.
func TestManager_CommittedDefinitionResolvesImmediately(t *testing.T) {
	manager, bus := newDIManager()
	ctx := awaitContext(t, bus)
	plane := contractsys.NewContractRegistry(bus, nil)
	require.NoError(t, plane.Start(ctxapi.NewRootContext()))
	t.Cleanup(func() { _ = plane.Stop() })
	defID := registry.NewID("test", "contract")

	for i := 0; i < 200; i++ {
		require.NoError(t, manager.Begin(ctx))
		require.NoError(t, manager.Add(ctx, definitionEntry(defID, "method1")))
		require.NoError(t, manager.Commit(ctx))
		_, err := plane.GetContract(ctx, defID)
		require.NoError(t, err, "iteration %d: committed definition must resolve", i)

		require.NoError(t, manager.Begin(ctx))
		require.NoError(t, manager.Delete(ctx, definitionEntry(defID)))
		require.NoError(t, manager.Commit(ctx))
		_, err = plane.GetContract(ctx, defID)
		assert.Error(t, err, "iteration %d: deleted definition must not resolve", i)
	}
}

// subscribeChanges calls handle with one event per change of the given kind
// in each change batch sent on bus.
func subscribeChanges(ctx context.Context, bus event.Bus, kind event.Kind, handle func(event.Event)) (*eventbus.Subscriber, error) {
	return eventbus.NewSubscriber(ctx, bus, contract.System, contract.ApplyChanges, func(batch event.Event) {
		changes, _ := batch.Data.(contract.Changes)
		for _, change := range changes {
			if change.Kind == kind {
				handle(event.Event{System: contract.System, Kind: change.Kind, Path: change.Path, Data: change.Data})
			}
		}
	})
}
