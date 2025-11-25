package std

import "github.com/wippyai/runtime/api/registry"

// ProcessSendHeader is the header payload for process.send commands.
// This is serialized as Params[0], with message payload in Params[1:].
type ProcessSendHeader struct {
	// Target is the destination process PID.
	Target string `json:"target"`

	// Topic is the message topic/channel name.
	Topic string `json:"topic"`
}

// ChildWorkflowHeader is the header payload for workflow.child commands.
// This is serialized as Params[0], with workflow input in Params[1:].
type ChildWorkflowHeader struct {
	// WorkflowID is the workflow registry ID to spawn.
	WorkflowID registry.ID `json:"workflow_id"`

	// Options contains execution options for the child workflow.
	Options *ChildWorkflowOptions `json:"options,omitempty"`
}

// ChildWorkflowOptions defines execution options for child workflows.
type ChildWorkflowOptions struct {
	// WorkflowExecutionTimeout as duration string.
	WorkflowExecutionTimeout string `json:"workflow_execution_timeout,omitempty"`

	// WorkflowRunTimeout as duration string.
	WorkflowRunTimeout string `json:"workflow_run_timeout,omitempty"`

	// WorkflowTaskTimeout as duration string.
	WorkflowTaskTimeout string `json:"workflow_task_timeout,omitempty"`

	// TaskQueue for routing.
	TaskQueue string `json:"task_queue,omitempty"`

	// WorkflowIDReusePolicy controls workflow ID reuse behavior.
	WorkflowIDReusePolicy string `json:"workflow_id_reuse_policy,omitempty"`

	// ParentClosePolicy determines what happens to child when parent closes.
	ParentClosePolicy string `json:"parent_close_policy,omitempty"`

	// Retry defines retry policy for the child workflow.
	Retry *RetryPolicy `json:"retry,omitempty"`
}
