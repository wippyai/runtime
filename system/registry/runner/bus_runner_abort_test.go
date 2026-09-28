// SPDX-License-Identifier: MPL-2.0

package runner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

// A failed transition runs abort exactly once, while the accepted operations
// are still applied, so the external state they were built against is
// withdrawn before the reverse operations run.
func TestBusRunner_AbortRunsOnceBeforeAcceptedOperationsAreReversed(t *testing.T) {
	ctx, _, busRunner, component, cleanup := setupTestEnvironment(t)
	defer cleanup()

	accepted := registry.ParseID("component/listener/key1")
	changeSet := registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: createEntry(accepted, "listener", "value1")},
		{Kind: registry.EntryCreate, Entry: createEntry(registry.ParseID("component/listener/key2"), "listener", "reject_this")},
	}

	aborts := 0
	var configAtAbort string
	var appliedAtAbort bool
	abort := func(context.Context) {
		aborts++
		configAtAbort, appliedAtAbort = component.getConfig(accepted)
	}

	finalState, err := busRunner.Transition(ctx, registry.State{}, changeSet, abort)
	require.Error(t, err)
	assert.Equal(t, 1, aborts)
	assert.True(t, appliedAtAbort, "abort runs before the accepted operation is reversed")
	assert.Equal(t, "value1", configAtAbort)
	_, stillApplied := component.getConfig(accepted)
	assert.False(t, stillApplied, "the accepted operation is reversed after abort")
	assert.Empty(t, finalState)
}

func TestBusRunner_SuccessfulTransitionDoesNotAbort(t *testing.T) {
	ctx, _, busRunner, _, cleanup := setupTestEnvironment(t)
	defer cleanup()

	aborted := false
	_, err := busRunner.Transition(ctx, registry.State{}, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: createEntry(registry.ParseID("component/listener/key1"), "listener", "value1")},
	}, func(context.Context) { aborted = true })
	require.NoError(t, err)
	assert.False(t, aborted)
}
