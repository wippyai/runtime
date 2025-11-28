// Package process2 provides engine2 process abstractions.
// This is the successor to api/process for new-style process execution.
package process2

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// StepStatus indicates the process state after Step() returns.
type StepStatus int

const (
	// StepContinue means the process yielded commands and expects to resume.
	StepContinue StepStatus = iota

	// StepIdle means the process is waiting for external input via Send().
	StepIdle

	// StepDone means the process has completed execution.
	StepDone
)

// MaxYields is the maximum yields per step that fit in the fixed buffer.
const MaxYields = 4

// StepResult is returned by Process.Step() containing status and yields.
type StepResult struct {
	Status     StepStatus
	yieldCount int
	yieldsBuf  [MaxYields]dispatcher.Command
	yields     []dispatcher.Command
}

// GetYields returns the yielded commands.
func (r *StepResult) GetYields() []dispatcher.Command {
	if r.yields != nil {
		return r.yields
	}
	return r.yieldsBuf[:r.yieldCount]
}

// AddYield appends a command to the result.
func (r *StepResult) AddYield(cmd dispatcher.Command) {
	if r.yieldCount < MaxYields {
		r.yieldsBuf[r.yieldCount] = cmd
		r.yieldCount++
	} else {
		if r.yields == nil {
			r.yields = make([]dispatcher.Command, MaxYields, MaxYields*2)
			copy(r.yields, r.yieldsBuf[:])
		}
		r.yields = append(r.yields, cmd)
	}
}

// YieldCount returns the number of yielded commands.
func (r *StepResult) YieldCount() int {
	if r.yields != nil {
		return len(r.yields)
	}
	return r.yieldCount
}

// Reset clears the result for reuse.
func (r *StepResult) Reset() {
	r.Status = StepContinue
	for i := 0; i < r.yieldCount; i++ {
		r.yieldsBuf[i] = nil
	}
	r.yieldCount = 0
	r.yields = nil
}

// YieldResults carries results from handler execution back to the process.
type YieldResults struct {
	Data  any
	Error error
}

var yieldResultsPool = sync.Pool{
	New: func() any { return &YieldResults{} },
}

// AcquireYieldResults gets a YieldResults from pool.
func AcquireYieldResults() *YieldResults {
	return yieldResultsPool.Get().(*YieldResults)
}

// ReleaseYieldResults returns a YieldResults to pool.
func ReleaseYieldResults(yr *YieldResults) {
	yr.Data = nil
	yr.Error = nil
	yieldResultsPool.Put(yr)
}

// Process is a schedulable unit of work implemented as a state machine.
// Processes are DATA, not goroutines. Workers call Step() to advance them.
type Process interface {
	// Execute starts execution with context, method and input.
	Execute(ctx context.Context, method string, input payload.Payloads) error

	// Step advances the process by one iteration.
	Step(results *YieldResults) (StepResult, error)

	// Close releases process resources.
	Close()

	// Receiver allows external messages to be sent to the process.
	relay.Receiver
}

// ProcessFactory creates new Process instances.
type ProcessFactory func() (Process, error)

// Executor provides blocking process execution.
type Executor interface {
	Execute(ctx context.Context, pid relay.PID, p Process, method string, input payload.Payloads) (*runtime.Result, error)
}
