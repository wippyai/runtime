package actor

import (
	"github.com/wippyai/runtime/api/process2"
)

// Re-export types from api/process2 for backward compatibility.
// New code should import api/process2 directly.
type (
	StepStatus     = process2.StepStatus
	StepResult     = process2.StepResult
	YieldResults   = process2.YieldResults
	Process        = process2.Process
	ProcessFactory = process2.ProcessFactory
)

// Re-export constants from api/process2.
const (
	StepContinue = process2.StepContinue
	StepIdle     = process2.StepIdle
	StepDone     = process2.StepDone
	MaxYields    = process2.MaxYields
)

// Re-export pool functions from api/process2.
var (
	AcquireYieldResults = process2.AcquireYieldResults
	ReleaseYieldResults = process2.ReleaseYieldResults
)
