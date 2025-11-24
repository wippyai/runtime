package workflow

import (
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
)

// Workflow extends Process with command-based execution for deterministic replay
type Workflow interface {
	process.Process
	Commands() []runtime.Command
}
