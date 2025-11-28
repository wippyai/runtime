package scheduler

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
)

// CommandID identifies a command type for O(1) handler lookup.
// Using uint8 limits to 256 command types but enables array-indexed dispatch.
// Subsystems reserve ranges: 0-9 core, 10-49 time, 50-99 io, 100-149 db, etc.
type CommandID uint8

// Command represents a yield from a process requesting external work.
// Commands are pure data - they carry no callbacks or internal references.
// The scheduler dispatches commands to handlers based on CmdID().
type Command interface {
	CmdID() CommandID
}

// StepStatus indicates the process state after Step() returns.
type StepStatus int

const (
	// StepContinue means the process yielded commands and expects to resume.
	// The scheduler should dispatch yielded commands to handlers.
	StepContinue StepStatus = iota

	// StepIdle means the process is waiting for external input via Send().
	// The scheduler should park the process until SendTo() is called.
	StepIdle

	// StepDone means the process has completed execution.
	// The scheduler should invoke completion callback and release resources.
	StepDone
)

// MaxYields is the maximum yields per step that fit in the fixed buffer.
// Steps yielding more than this will allocate a slice (rare case).
const MaxYields = 4

// StepResult is returned by Process.Step() containing status and yields.
// Uses fixed-size buffer to avoid allocation in the common case (<=4 yields).
type StepResult struct {
	Status     StepStatus
	YieldCount int                // Number of valid yields in YieldsBuf
	YieldsBuf  [MaxYields]Command // Fixed buffer for zero-alloc common case
	Yields     []Command          // Overflow slice for >MaxYields (rare)
}

// GetYields returns the yielded commands, using buffer or slice as appropriate.
func (r *StepResult) GetYields() []Command {
	if r.Yields != nil {
		return r.Yields
	}
	return r.YieldsBuf[:r.YieldCount]
}

// YieldResults carries results from handler execution back to the process.
// Passed to Step() so the process can receive completed yield data.
type YieldResults struct {
	Data  any   // Result data from handler (nil if none)
	Error error // Error from handler (nil on success)
}

// Process is a schedulable unit of work implemented as a state machine.
// Processes are DATA, not goroutines. Workers call Step() to advance them.
//
// The scheduler owns the execution loop:
//  1. Call Start() to initialize
//  2. Call Step(nil) for first iteration
//  3. Dispatch yielded commands to handlers
//  4. When handler completes, call Step(results)
//  5. Repeat until StepDone
//
// Process implementations should be stateless regarding yield tracking.
// All yield-to-callback mapping belongs in Frame Context, not the process.
type Process interface {
	// Start initializes the process with context and input payloads.
	// PID is stored in Processor wrapper, accessible via frame context.
	// Returns error if initialization fails.
	Start(ctx context.Context, input payload.Payloads) error

	// Step advances the process by one iteration.
	// First call receives nil results. Subsequent calls receive handler results.
	// Returns status indicating next action and any yielded commands.
	Step(results *YieldResults) (StepResult, error)

	// Send delivers an external message to the process.
	// Used for events like signals, not for yield results.
	// Only valid when process is in StepIdle state.
	Send(pkg *relay.Package) error
}

// Handler executes commands yielded by processes.
// Handlers are registered per CommandID and invoked by the scheduler.
//
// Handlers MUST:
//   - Call proc.Complete(data, err) exactly once when done
//   - For sync ops: call Complete() before returning
//   - For async ops: spawn goroutine, call Complete() when finished
//
// Handlers MUST NOT:
//   - Access frame context or internal process state
//   - Hold references to proc after calling Complete()
//   - Call Complete() more than once
type Handler interface {
	Handle(cmd Command, proc *Processor)
}

// HandlerFunc is an adapter to use functions as Handler interface.
type HandlerFunc func(cmd Command, proc *Processor)

// Handle implements Handler interface.
func (f HandlerFunc) Handle(cmd Command, proc *Processor) {
	f(cmd, proc)
}
