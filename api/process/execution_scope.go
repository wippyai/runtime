// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/pid"
	"go.uber.org/zap"
)

// Ownership errors.
var (
	// ErrOwnerRequired fails an owned spawn from a context without an
	// execution scope.
	ErrOwnerRequired = apierror.New(InvalidState, "owned spawn requires an owning execution").WithRetryable(apierror.False)
	// ErrOwnerEnded fails an owned spawn whose owning execution has ended.
	ErrOwnerEnded = apierror.New(InvalidState, "owning execution has ended").WithRetryable(apierror.False)
)

var (
	// executionScopeKey holds the scope of the execution running in a frame.
	// It is not inherited: every execution gets its own scope.
	executionScopeKey = &ctxapi.Key{Name: "process.execution_scope", Execution: true}
	// ownedKey marks the execution of an owned process.
	ownedKey = &ctxapi.Key{Name: "process.owned", Execution: true}
	// ownedChildKey holds, in an owned process's frame, its registration
	// with its owner's scope; the frame releases it.
	ownedChildKey = &ctxapi.Key{Name: "process.owned_child"}
)

// Terminator stops processes; Manager implements it.
type Terminator interface {
	Terminate(ctx context.Context, p pid.PID) error
}

// ExecutionKind is the kind of execution a scope belongs to.
type ExecutionKind uint8

const (
	// ExecutionProcess is a process: it ends when the process completes.
	ExecutionProcess ExecutionKind = iota
	// ExecutionFunction is one function call: it ends when the call returns.
	ExecutionFunction
)

// ExecutionScope is the lifetime of one execution: a process or a single
// function call. Processes it owns cannot outlive it: when the execution
// completes, the scope refuses new owned children and terminates the ones
// still running. The scope ends at completion, independently of frame
// reclamation, which waits for frames forked from it.
type ExecutionScope struct {
	terminator Terminator
	ctx        context.Context
	owned      map[*ownedChild]struct{}
	mu         sync.Mutex
	kind       ExecutionKind
	ended      bool
}

// NewExecutionScope returns the scope of an execution that terminates its
// owned processes through terminator.
func NewExecutionScope(ctx context.Context, kind ExecutionKind, terminator Terminator) *ExecutionScope {
	return &ExecutionScope{
		terminator: terminator,
		ctx:        context.WithoutCancel(ctx),
		kind:       kind,
	}
}

// NewExecutionScopeFor returns the scope of a new execution that terminates
// owned processes through the process manager in ctx, or nil when ctx has
// none; owned spawns from an execution without a scope fail.
func NewExecutionScopeFor(ctx context.Context, kind ExecutionKind) *ExecutionScope {
	manager := GetManager(ctx)
	if manager == nil {
		return nil
	}
	return NewExecutionScope(ctx, kind, manager)
}

// ExecutionScopePair installs scope on an execution frame.
func ExecutionScopePair(scope *ExecutionScope) ctxapi.Pair {
	return ctxapi.Pair{Key: executionScopeKey, Value: scope}
}

// GetExecutionScope returns the scope of the execution in ctx, or nil.
func GetExecutionScope(ctx context.Context) *ExecutionScope {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if v, ok := fc.Get(executionScopeKey); ok {
		scope, _ := v.(*ExecutionScope)
		return scope
	}
	return nil
}

// IsOwned reports whether the execution in ctx is an owned process.
// Everything an owned process spawns is owned by it in turn.
func IsOwned(ctx context.Context) bool {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return false
	}
	return fc.Has(ownedKey)
}

// Kind returns the kind of execution the scope belongs to.
func (s *ExecutionScope) Kind() ExecutionKind {
	return s.kind
}

// Reserve registers a child about to start. The returned pairs go into the
// child's frame: they mark the child owned and hold its registration as an
// attachment, rolled back if the child is not admitted and released when the
// child completes.
func (s *ExecutionScope) Reserve() ([]ctxapi.Pair, *OwnedChild, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return nil, nil, ErrOwnerEnded
	}
	child := &ownedChild{scope: s}
	if s.owned == nil {
		s.owned = make(map[*ownedChild]struct{}, 2)
	}
	s.owned[child] = struct{}{}
	pairs := []ctxapi.Pair{
		{Key: ownedKey, Value: true},
		{Key: ownedChildKey, Value: child},
	}
	return pairs, &OwnedChild{child: child}, nil
}

// Complete implements ctxapi.Completer: the execution has ended. Owned
// children still running are terminated; later reservations fail.
func (s *ExecutionScope) Complete() {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	var running []pid.PID
	for child := range s.owned {
		child.ended = true
		if child.bound {
			running = append(running, child.pid)
		}
	}
	s.owned = nil
	s.mu.Unlock()
	for _, p := range running {
		s.terminate(p)
	}
}

// terminate stops an owned child. A child that already exited needs
// nothing; any other failure is logged, since completion has no caller to
// report to.
func (s *ExecutionScope) terminate(p pid.PID) {
	err := s.terminator.Terminate(s.ctx, p)
	if err == nil || errors.Is(err, ErrProcessNotFound) {
		return
	}
	logs.GetLogger(s.ctx).Error("failed to terminate owned process",
		zap.String("pid", p.String()), zap.Error(err))
}

// OwnedChild binds a reserved child to the PID it started with.
type OwnedChild struct {
	child *ownedChild
}

// Bind records the started child's PID. It returns ErrOwnerEnded when the
// owner ended while the child was starting; the caller must stop the child.
func (o *OwnedChild) Bind(p pid.PID) error {
	c := o.child
	c.scope.mu.Lock()
	defer c.scope.mu.Unlock()
	if c.ended {
		return ErrOwnerEnded
	}
	c.pid = p
	c.bound = true
	return nil
}

// ownedChild is a child's registration with its owner's scope.
type ownedChild struct {
	scope *ExecutionScope
	pid   pid.PID
	bound bool
	ended bool
}

// Complete releases the registration when the child completes.
func (c *ownedChild) Complete() { c.release() }

// Close releases the registration when the child's frame is released.
func (c *ownedChild) Close() error {
	c.release()
	return nil
}

// Rollback releases the registration when the child is not admitted.
func (c *ownedChild) Rollback() error {
	c.release()
	return nil
}

func (c *ownedChild) release() {
	c.scope.mu.Lock()
	defer c.scope.mu.Unlock()
	delete(c.scope.owned, c)
}
