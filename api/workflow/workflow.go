package workflow

import (
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/workflow/std"
)

// Workflow extends Process with command-based execution for deterministic replay
type Workflow interface {
	process.Process
	Commands() []runtime.Command
}

// TaskReceiver is an optional interface for workflows that can receive tasks.
// Hosts type-assert to this interface to push tasks directly to workflows.
type TaskReceiver interface {
	// PushTask delivers a task to the workflow for processing.
	// The workflow processes the task and calls task.Complete() or task.Fail().
	PushTask(task std.Task) error
}
