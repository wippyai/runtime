// Package scheduler re-exports process2 types for backwards compatibility.
// New code should import api/process2 directly.
package scheduler

import (
	"github.com/wippyai/runtime/api/process2"
)

// Re-export process types for backwards compatibility.
type (
	StepStatus     = process2.StepStatus
	StepResult     = process2.StepResult
	YieldResults   = process2.YieldResults
	Process        = process2.Process
	ProcessFactory = process2.ProcessFactory
	Executor       = process2.Executor
)

// Re-export constants.
const (
	StepContinue = process2.StepContinue
	StepIdle     = process2.StepIdle
	StepDone     = process2.StepDone
	MaxYields    = process2.MaxYields
)

// Re-export functions.
var (
	AcquireYieldResults = process2.AcquireYieldResults
	ReleaseYieldResults = process2.ReleaseYieldResults
)
