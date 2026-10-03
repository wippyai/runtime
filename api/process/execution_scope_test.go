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

	exitedPair, exited, err := scope.Reserve()
	require.NoError(t, err)
	require.NoError(t, exited.Bind(pid.PID{Host: "h", UniqID: "exited"}))
	exitedPair.Value.(ctxapi.Completer).Complete()

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
	pair, _, err := scope.Reserve()
	require.NoError(t, err)
	require.NoError(t, pair.Value.(ctxapi.FrameAttachment).Rollback())
	scope.Complete()
	require.Empty(t, term.terminated)
}

func TestExecutionScopeFrameHelpers(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)
	require.Nil(t, GetExecutionScope(ctx))
	require.False(t, IsOwned(ctx))

	scope := NewExecutionScope(ctx, ExecutionProcess, &recordingTerminator{})
	pair, _, err := scope.Reserve()
	require.NoError(t, err)
	require.NoError(t, fc.SetMultiple(ExecutionScopePair(scope), pair))
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
