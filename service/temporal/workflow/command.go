package workflow

import (
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
)

// Command IDs for Temporal-specific workflow operations.
// These are processed by the workflow definition, not the standard scheduler.
// Timer/sleep uses the standard clock.CmdSleep command.
const (
	CmdActivity      process.CommandID = 300
	CmdLocalActivity process.CommandID = 301
	CmdChildWorkflow process.CommandID = 302
	CmdSignal        process.CommandID = 303 // Signal external workflow (outgoing only)
)

func init() {
	process.MustRegisterCommands("temporal.workflow",
		CmdActivity,
		CmdLocalActivity,
		CmdChildWorkflow,
		CmdSignal,
	)
}

// ActivityCommand requests execution of a Temporal activity.
type ActivityCommand struct {
	Name    string           `json:"name"`
	Options *ActivityOptions `json:"options,omitempty"`
	Args    payload.Payloads `json:"args,omitempty"`
}

func (c *ActivityCommand) CmdID() process.CommandID { return CmdActivity }

// LocalActivityCommand requests execution of a local activity.
type LocalActivityCommand struct {
	Name    string                `json:"name"`
	Options *LocalActivityOptions `json:"options,omitempty"`
	Args    payload.Payloads      `json:"args,omitempty"`
}

func (c *LocalActivityCommand) CmdID() process.CommandID { return CmdLocalActivity }

// ChildWorkflowCommand requests execution of a child workflow.
type ChildWorkflowCommand struct {
	Name    string                `json:"name"`
	Options *ChildWorkflowOptions `json:"options,omitempty"`
	Args    payload.Payloads      `json:"args,omitempty"`
}

func (c *ChildWorkflowCommand) CmdID() process.CommandID { return CmdChildWorkflow }

// SignalCommand sends a signal to an external workflow.
type SignalCommand struct {
	WorkflowID string          `json:"workflow_id"`
	RunID      string          `json:"run_id,omitempty"`
	SignalName string          `json:"signal_name"`
	Arg        payload.Payload `json:"arg,omitempty"`
}

func (c *SignalCommand) CmdID() process.CommandID { return CmdSignal }
