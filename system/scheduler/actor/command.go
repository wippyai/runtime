package actor

import (
	"github.com/wippyai/runtime/api/process"
)

// Re-export types from api/process for backward compatibility.
// New code should import api/process directly.
type (
	StepStatus     = process.StepStatus
	StepOutput     = process.StepOutput
	Event          = process.Event
	EventType      = process.EventType
	Process        = process.Process
	ProcessFactory = process.FactoryFunc
	Yield          = process.Yield
	EventQueue     = process.EventQueue
)

// Re-export constants from api/process.
const (
	StepContinue       = process.StepContinue
	StepIdle           = process.StepIdle
	StepDone           = process.StepDone
	MaxYields          = process.MaxYields
	EventYieldComplete = process.EventYieldComplete
	EventMessage       = process.EventMessage
)
