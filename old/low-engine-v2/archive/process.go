package engine

import "context"

// CommandID is uint8 for O(1) array-indexed handler lookup.
// Subsystems define their own command IDs in reserved ranges.
type CommandID uint8

// Command is the yield interface - processes return these to request operations.
// Concrete command types are defined by subsystems, not here.
type Command interface {
	CmdID() CommandID
}

// StepStatus indicates execution state after a step.
type StepStatus int

const (
	StepContinue StepStatus = iota // has yields to execute
	StepIdle                       // waiting for external event (Send)
	StepDone                       // process completed
)

// MaxYields is the max yields per step without allocation.
const MaxYields = 4

// StepResult is returned by Process.Step().
// Uses fixed array to avoid slice allocation in hot path.
type StepResult struct {
	Status     StepStatus
	YieldCount int                // number of valid yields
	YieldsBuf  [MaxYields]Command // fixed buffer, use YieldCount to know how many
	Yields     []Command          // for >MaxYields case (rare), or nil to use YieldsBuf
}

// GetYields returns the yields, preferring the buffer.
func (r *StepResult) GetYields() []Command {
	if r.Yields != nil {
		return r.Yields
	}
	return r.YieldsBuf[:r.YieldCount]
}

// YieldResults carries results from handler execution back to the process.
type YieldResults struct {
	Data  any
	Error error
}

// Process is a schedulable unit of work. It's DATA, not a goroutine.
// Workers pop processes from queues and call Step() to advance them.
type Process interface {
	// Start initializes the process with context and input.
	// The context is stored and used for all future yields (cancellation propagates).
	Start(ctx context.Context, input any) error

	// Step advances the process by one iteration.
	// Takes results from previous yield execution (nil on first call).
	// Returns status and next yields to execute.
	Step(yieldResults *YieldResults) (StepResult, error)

	// Send delivers a message to the process (like relay.Receiver).
	// Used for external events, not for yield results.
	Send(pkg *Package) error
}

// Package is a message delivered to a process via Send.
type Package struct {
	Type    string
	Payload any
}
