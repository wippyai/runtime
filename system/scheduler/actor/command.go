package actor

import (
	"github.com/wippyai/runtime/api/scheduler"
)

// Re-export types from api/scheduler for backward compatibility.
// New code should import api/scheduler directly.
type (
	StepStatus     = scheduler.StepStatus
	StepResult     = scheduler.StepResult
	YieldResults   = scheduler.YieldResults
	Process        = scheduler.Process
	ProcessFactory = scheduler.ProcessFactory
)

// Re-export constants from api/scheduler.
const (
	StepContinue = scheduler.StepContinue
	StepIdle     = scheduler.StepIdle
	StepDone     = scheduler.StepDone
	MaxYields    = scheduler.MaxYields
)

// Re-export pool functions from api/scheduler.
var (
	AcquireYieldResults = scheduler.AcquireYieldResults
	ReleaseYieldResults = scheduler.ReleaseYieldResults
)
