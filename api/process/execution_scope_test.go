// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
)

type recordingTerminator struct {
	err        error
	terminated []pid.PID
	mu         sync.Mutex
}

func (r *recordingTerminator) Terminate(_ context.Context, p pid.PID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.terminated = append(r.terminated, p)
	return r.err
}

func TestExecutionScopeTerminatesRunningChildrenWhenItEnds(t *testing.T) {
	term := &recordingTerminator{}
	scope := NewExecutionScope(context.Background(), ExecutionFunction, term)

	_, running, err := scope.Reserve()
	require.NoError(t, err)
	child := pid.PID{Host: "h", UniqID: "child"}
	require.NoError(t, running.Bind(child))

	exitedPairs, exited, err := scope.Reserve()
	require.NoError(t, err)
	require.NoError(t, exited.Bind(pid.PID{Host: "h", UniqID: "exited"}))
	registration(t, exitedPairs).(ctxapi.Completer).Complete()

	scope.Complete()
	require.Equal(t, []pid.PID{child}, term.terminated, "only children still running are terminated")

	scope.Complete()
	require.Len(t, term.terminated, 1, "ending is idempotent")
}

func TestExecutionScopeRefusesChildrenAfterItEnds(t *testing.T) {
	scope := NewExecutionScope(context.Background(), ExecutionProcess, &recordingTerminator{})
	scope.Complete()
	_, _, err := scope.Reserve()
	require.ErrorIs(t, err, ErrOwnerEnded)
}

func TestExecutionScopeChildStartingWhileOwnerEnds(t *testing.T) {
	term := &recordingTerminator{}
	scope := NewExecutionScope(context.Background(), ExecutionProcess, term)
	_, starting, err := scope.Reserve()
	require.NoError(t, err)

	scope.Complete()
	require.Empty(t, term.terminated, "an unbound child has no PID to terminate yet")
	require.ErrorIs(t, starting.Bind(pid.PID{Host: "h", UniqID: "late"}), ErrOwnerEnded,
		"the starter learns the owner ended and stops the child")
}

func TestExecutionScopeRollbackReleasesReservation(t *testing.T) {
	term := &recordingTerminator{}
	scope := NewExecutionScope(context.Background(), ExecutionProcess, term)
	pairs, _, err := scope.Reserve()
	require.NoError(t, err)
	require.NoError(t, registration(t, pairs).(ctxapi.FrameAttachment).Rollback())
	scope.Complete()
	require.Empty(t, term.terminated)
}

func TestExecutionScopeFrameHelpers(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)
	require.Nil(t, GetExecutionScope(ctx))
	require.False(t, IsOwned(ctx))

	scope := NewExecutionScope(ctx, ExecutionProcess, &recordingTerminator{})
	pairs, _, err := scope.Reserve()
	require.NoError(t, err)
	require.NoError(t, fc.SetMultiple(append(pairs, ExecutionScopePair(scope))...))
	require.Same(t, scope, GetExecutionScope(ctx))
	require.True(t, IsOwned(ctx))

	forked, ffc := ctxapi.ForkFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(ffc)
	require.Nil(t, GetExecutionScope(forked), "every execution gets its own scope")
	require.False(t, IsOwned(forked), "ownership is per process")

	require.Nil(t, NewExecutionScopeFor(context.Background(), ExecutionFunction), "no manager, no scope")
}

func TestExecutionScopeCompletesThroughFrame(t *testing.T) {
	term := &recordingTerminator{}
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)
	scope := NewExecutionScope(ctx, ExecutionProcess, term)
	require.NoError(t, fc.SetMultiple(ExecutionScopePair(scope)))
	_, child, err := scope.Reserve()
	require.NoError(t, err)
	require.NoError(t, child.Bind(pid.PID{Host: "h", UniqID: "c"}))

	ctxapi.CompleteFrame(ctx)
	require.Len(t, term.terminated, 1, "process completion ends the scope")
}

// registration returns the owned child's registration among its frame pairs.
func registration(t *testing.T, pairs []ctxapi.Pair) any {
	t.Helper()
	for _, p := range pairs {
		if p.Key == ownedChildKey {
			return p.Value
		}
	}
	t.Fatal("reservation holds no registration")
	return nil
}

func TestExecutionScopeSurvivesUpgrade(t *testing.T) {
	term := &recordingTerminator{}
	owner := NewExecutionScope(context.Background(), ExecutionProcess, term)
	pairs, _, err := owner.Reserve()
	require.NoError(t, err)

	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)
	scope := NewExecutionScope(ctx, ExecutionProcess, term)
	slots := NewChildSlots(2)
	require.NoError(t, fc.SetMultiple(append(pairs, ExecutionScopePair(scope), ChildSlotsPair(slots))...))
	fc.Seal()

	upgraded, ufc, err := ctxapi.ContinueFrameContext(ctx)
	require.NoError(t, err)
	defer ctxapi.ReleaseFrameContext(ufc)
	require.Same(t, scope, GetExecutionScope(upgraded), "the upgraded code runs in the same execution")
	require.True(t, IsOwned(upgraded), "an owned process stays owned across upgrades")
	got, ok := ufc.Get(childSlotsKey)
	require.True(t, ok)
	require.Same(t, slots, got, "the child limit spans the whole process")
	require.False(t, ufc.Has(ownedChildKey), "the registration stays with the execution's frame")
}

func TestOwnedChildBindAfterRollback(t *testing.T) {
	term := &recordingTerminator{}
	scope := NewExecutionScope(context.Background(), ExecutionProcess, term)
	pairs, child, err := scope.Reserve()
	require.NoError(t, err)
	require.NoError(t, registration(t, pairs).(ctxapi.FrameAttachment).Rollback())
	require.ErrorIs(t, child.Bind(pid.PID{Host: "h", UniqID: "existing"}), ErrOwnedNotStarted)

	fast, fastChild, err := scope.Reserve()
	require.NoError(t, err)
	registration(t, fast).(ctxapi.Completer).Complete()
	require.NoError(t, fastChild.Bind(pid.PID{Host: "h", UniqID: "fast"}), "a child may complete before it is bound")
	scope.Complete()
	require.Empty(t, term.terminated)
}
