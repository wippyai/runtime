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
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
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

// gatedPlane answers contract-plane events with contract.accept only when the
// test releases it, so a test can observe whether the manager returns before
// the plane has applied a change.
type gatedPlane struct {
	bus      event.Bus
	received chan event.Event
}

func newGatedPlane(t *testing.T, bus event.Bus) *gatedPlane {
	t.Helper()
	plane := &gatedPlane{bus: bus, received: make(chan event.Event, 16)}
	sub, err := eventbus.NewSubscriber(ctxapi.NewRootContext(), bus, contract.System,
		"contract.(definition|binding).(register|update|delete)",
		func(evt event.Event) { plane.received <- evt })
	require.NoError(t, err)
	t.Cleanup(sub.Close)
	return plane
}

func (p *gatedPlane) accept(t *testing.T) {
	t.Helper()
	select {
	case evt := <-p.received:
		p.bus.Send(ctxapi.NewRootContext(), event.Event{System: contract.System, Kind: contract.ContractAccept, Path: evt.Path})
	case <-time.After(5 * time.Second):
		t.Fatal("contract plane received no event")
	}
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
// returns: Commit completes only once the contract plane has acknowledged
// every event of the transaction.
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

	requirePending(t, done, "Commit")
	plane.accept(t)
	requirePending(t, done, "Commit")
	plane.accept(t)
	requireReturned(t, done, "Commit")
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
