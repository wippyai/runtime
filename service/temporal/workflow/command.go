package workflow

import (
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
)

// Command IDs for Temporal-specific workflow operations.
// These are processed by the workflow definition, not the standard scheduler.
// Timer/sleep uses the standard clock.Sleep command.
const (
	Activity      dispatcher.CommandID = 400
	LocalActivity dispatcher.CommandID = 401
	ChildWorkflow dispatcher.CommandID = 402
	Signal        dispatcher.CommandID = 403 // Signal external workflow (outgoing only)
)

func init() {
	dispatcher.MustRegisterCommands("temporal.workflow",
		Activity,
		LocalActivity,
		ChildWorkflow,
		Signal,
	)
}

// ActivityCmd requests execution of a Temporal activity.
type ActivityCmd struct {
	Name    string           `json:"name"`
	Options *ActivityOptions `json:"options,omitempty"`
	Args    payload.Payloads `json:"args,omitempty"`
}

func (c *ActivityCmd) CmdID() dispatcher.CommandID { return Activity }

// LocalActivityCmd requests execution of a local activity.
type LocalActivityCmd struct {
	Name    string                `json:"name"`
	Options *LocalActivityOptions `json:"options,omitempty"`
	Args    payload.Payloads      `json:"args,omitempty"`
}

func (c *LocalActivityCmd) CmdID() dispatcher.CommandID { return LocalActivity }

// ChildWorkflowCmd requests execution of a child workflow.
type ChildWorkflowCmd struct {
	Name    string                `json:"name"`
	Options *ChildWorkflowOptions `json:"options,omitempty"`
	Args    payload.Payloads      `json:"args,omitempty"`
}

func (c *ChildWorkflowCmd) CmdID() dispatcher.CommandID { return ChildWorkflow }

// SignalCmd sends a signal to an external workflow.
type SignalCmd struct {
	WorkflowID string          `json:"workflow_id"`
	RunID      string          `json:"run_id,omitempty"`
	SignalName string          `json:"signal_name"`
	Arg        payload.Payload `json:"arg,omitempty"`
}

func (c *SignalCmd) CmdID() dispatcher.CommandID { return Signal }
