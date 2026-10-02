// SPDX-License-Identifier: MPL-2.0

package runner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type privateTestBuilder struct {
	*testBuilder
	privateCalls  int
	snapshotCalls int
}

func (b *privateTestBuilder) ApplyOperation(state registry.StateMap, op registry.Operation) (registry.StateMap, error) {
	b.snapshotCalls++
	return b.testBuilder.ApplyOperation(state, op)
}

func (b *privateTestBuilder) ApplyOperationToPrivateState(state registry.StateMap, op registry.Operation) error {
	if err := b.ValidateOperation(state, op); err != nil {
		return err
	}
	b.privateCalls++
	switch op.Kind {
	case registry.EntryCreate, registry.EntryUpdate:
		state[op.Entry.ID] = op.Entry
	case registry.EntryDelete:
		delete(state, op.Entry.ID)
	}
	return nil
}

func TestTransitionOwnsItsWorkingMapAndRetainsRollbackState(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	defer bus.Stop()
	builder := &privateTestBuilder{testBuilder: newTestBuilder(nil)}
	runner := NewBusRunner(bus, zap.NewNop(), builder, WithDispatchPolicy(internalDispatchPolicy()))
	a := createEntry(registry.NewID("test", "a"), registry.EntryKind, "initial")
	b := registry.Entry{ID: registry.NewID("test", "b"), Kind: registry.EntryKind}
	initial := registry.State{a}
	updated := createEntry(a.ID, a.Kind, "updated")
	changes := registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: updated}, {Kind: registry.EntryCreate, Entry: b}}
	result, err := runner.Transition(ctx, initial, changes, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, registry.State{updated, b}, result)
	require.Equal(t, registry.State{a}, initial)
	require.Equal(t, 2, builder.privateCalls, "accepted operations use the transaction's private state")
	require.Zero(t, builder.snapshotCalls, "private transitions do not copy the whole map per entry")
	changes = append(changes, registry.Operation{Kind: registry.EntryCreate, Entry: b})
	rolled, err := runner.Transition(ctx, initial, changes, nil)
	require.Error(t, err)
	require.Equal(t, initial, rolled)
	require.Equal(t, registry.State{a}, initial)
	require.Zero(t, builder.snapshotCalls)
}

func TestPrivateStateChangesOnlyAfterListenerAcceptanceAndRollsBackEffects(t *testing.T) {
	ctx, _, runner, component, cleanup := setupTestEnvironment(t)
	defer cleanup()
	builder := &privateTestBuilder{testBuilder: newTestBuilder(nil)}
	runner.builder = builder
	a := createEntry(registry.NewID("test", "a"), "listener", "initial")
	initial, err := runner.Transition(ctx, nil, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: a}}, nil)
	require.NoError(t, err)
	updated := createEntry(a.ID, a.Kind, "accepted")
	rejected := createEntry(registry.NewID("test", "b"), "listener", "reject-not-admitted")
	rolled, err := runner.Transition(ctx, initial, registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: updated}, {Kind: registry.EntryCreate, Entry: rejected},
	}, nil)
	require.Error(t, err)
	require.Equal(t, registry.State{a}, initial)
	require.Equal(t, initial, rolled)
	component.mu.RLock()
	require.Equal(t, "initial", component.config[a.ID])
	require.NotContains(t, component.config, rejected.ID)
	component.mu.RUnlock()
	require.Equal(t, 3, builder.privateCalls, "seed, accepted update, and compensating update only")
	require.Zero(t, builder.snapshotCalls)
}
