// Package engine provides WASM process implementation for wippy scheduler.
package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/runtime/api/dispatcher"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
)

// yieldResultPool reduces allocations for YieldResult in hot path.
var yieldResultPool = sync.Pool{
	New: func() any { return &YieldResult{} },
}

// Scheduler manages async WASM execution with step-based control.
// Yields dispatcher.Command directly without wrapping.
type Scheduler struct {
	asyncify *wasmengine.Asyncify

	fn   api.Function
	args []uint64

	// Direct dispatcher.Command storage - no PendingOp wrapping
	pendingCmd dispatcher.Command
	result     uint64
	err        error

	initialized bool
}

// NewScheduler creates a new async scheduler.
func NewScheduler(asyncify *wasmengine.Asyncify) *Scheduler {
	return &Scheduler{
		asyncify: asyncify,
	}
}

// SetPending stores a dispatcher.Command to be yielded.
// Implements dispatcher.AsyncScheduler interface.
func (s *Scheduler) SetPending(cmd dispatcher.Command) {
	s.pendingCmd = cmd
}

// GetResult returns the result of the last completed operation.
func (s *Scheduler) GetResult() (uint64, error) {
	return s.result, s.err
}

// ClearPending clears the pending command.
func (s *Scheduler) ClearPending() {
	s.pendingCmd = nil
	s.result = 0
	s.err = nil
}

// Execute initializes execution of a function. Call Step() to advance.
func (s *Scheduler) Execute(ctx context.Context, fn api.Function, args ...uint64) error {
	if !s.asyncify.IsNormal(ctx) {
		return NewSchedulerStateError("asyncify not in normal state")
	}
	s.fn = fn
	s.args = args
	s.initialized = true
	s.asyncify.ResetStack()
	return nil
}

// SchedulerStatus indicates the scheduler state after Step().
type SchedulerStatus int

const (
	SchedulerContinue SchedulerStatus = iota
	SchedulerDone
)

// SchedulerResult is returned by Scheduler.Step().
type SchedulerResult struct {
	Status  SchedulerStatus
	Command dispatcher.Command // Direct command, no wrapping
	Results []uint64
}

// YieldResult carries the result from handler execution back to the scheduler.
type YieldResult struct {
	Value uint64
	Error error
}

// AcquireYieldResult gets a YieldResult from the pool.
func AcquireYieldResult() *YieldResult {
	return yieldResultPool.Get().(*YieldResult)
}

// ReleaseYieldResult returns a YieldResult to the pool.
func ReleaseYieldResult(yr *YieldResult) {
	yr.Value = 0
	yr.Error = nil
	yieldResultPool.Put(yr)
}

// Step advances execution by one iteration.
func (s *Scheduler) Step(ctx context.Context, yr *YieldResult) (SchedulerResult, error) {
	if err := ctx.Err(); err != nil {
		return SchedulerResult{}, err
	}
	if !s.initialized {
		return SchedulerResult{}, NewSchedulerStateError("call Execute first")
	}

	// If resuming with results, set them and start rewind
	if yr != nil {
		fmt.Printf("DEBUG scheduler.Step: resuming with result=%d, err=%v\n", yr.Value, yr.Error)
		s.result = yr.Value
		s.err = yr.Error
		if s.err != nil {
			return SchedulerResult{}, s.err
		}
		if err := s.asyncify.StartRewind(ctx); err != nil {
			return SchedulerResult{}, NewSchedulerRewindError(err)
		}
		// Note: keep s.args - wazero requires the correct parameter count
		// even though asyncify restores actual values from stack
	}

	// Call the function
	fmt.Printf("DEBUG scheduler.Step: calling fn with args=%v\n", s.args)
	results, callErr := s.fn.Call(ctx, s.args...)
	fmt.Printf("DEBUG scheduler.Step: fn.Call returned results=%v, err=%v\n", results, callErr)

	// Check if we're unwinding (handler triggered suspend)
	isUnwinding := s.asyncify.IsUnwinding(ctx)
	fmt.Printf("DEBUG scheduler.Step: isUnwinding=%v, pendingCmd=%v\n", isUnwinding, s.pendingCmd != nil)

	if isUnwinding {
		if err := s.asyncify.StopUnwind(ctx); err != nil {
			return SchedulerResult{}, NewSchedulerUnwindError(err)
		}
		if s.pendingCmd == nil {
			return SchedulerResult{}, NewSchedulerStateError("no pending command after unwind")
		}
		cmd := s.pendingCmd
		s.pendingCmd = nil
		fmt.Printf("DEBUG scheduler.Step: yielding command type=%T\n", cmd)
		return SchedulerResult{Status: SchedulerContinue, Command: cmd}, nil
	}

	// Normal completion or real error
	if callErr != nil {
		fmt.Printf("DEBUG scheduler.Step: call error=%v\n", callErr)
		return SchedulerResult{}, callErr
	}

	// Verify we're back to normal state
	if !s.asyncify.IsNormal(ctx) {
		return SchedulerResult{}, NewSchedulerStateError("unexpected state after call")
	}

	fmt.Printf("DEBUG scheduler.Step: done, results=%v\n", results)
	s.initialized = false
	return SchedulerResult{Status: SchedulerDone, Results: results}, nil
}

// Reset clears scheduler state for reuse.
func (s *Scheduler) Reset() {
	s.fn = nil
	s.args = nil
	s.pendingCmd = nil
	s.result = 0
	s.err = nil
	s.initialized = false
}

// WithScheduler adds scheduler to context.
func WithScheduler(ctx context.Context, s *Scheduler) context.Context {
	return wasmapi.WithScheduler(ctx, s)
}

// GetScheduler retrieves scheduler from context.
func GetScheduler(ctx context.Context) *Scheduler {
	if v := wasmapi.GetScheduler(ctx); v != nil {
		return v.(*Scheduler)
	}
	return nil
}

// WithAsyncify adds asyncify to context.
func WithAsyncify(ctx context.Context, a *wasmengine.Asyncify) context.Context {
	return wasmapi.WithAsyncify(ctx, a)
}

// GetAsyncify retrieves asyncify from context.
func GetAsyncify(ctx context.Context) *wasmengine.Asyncify {
	return wasmapi.GetAsyncify(ctx)
}

// Compile-time check that Scheduler implements dispatcher.AsyncScheduler
var _ dispatcher.AsyncScheduler = (*Scheduler)(nil)
