package actor

import (
	"github.com/wippyai/runtime/api/process"
)

// Re-export types from api/process for backward compatibility.
// New code should import api/process directly.
type (
	StepStatus     = process.StepStatus
	StepResult     = process.StepResult
	YieldResults   = process.YieldResults
	Process        = process.Process
	ProcessFactory = process.NewFunc
)

// Re-export constants from api/process.
const (
	StepContinue = process.StepContinue
	StepIdle     = process.StepIdle
	StepDone     = process.StepDone
	MaxYields    = process.MaxYields
)

// Re-export pool functions from api/process.
var (
	AcquireYieldResults = process.AcquireYieldResults
	ReleaseYieldResults = process.ReleaseYieldResults
)
