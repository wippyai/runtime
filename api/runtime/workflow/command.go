package workflow

import (
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

func init() {
	dispatcher.MustRegisterCommands("workflow", SideEffect, Call, Version, UpsertAttrs)
}

// Command IDs for workflow operations.
const (
	// SideEffect is the command ID for workflow side effect operations.
	SideEffect dispatcher.CommandID = 180
	// Call is the command ID for synchronous child workflow execution.
	Call dispatcher.CommandID = 181
	// Version is the command ID for workflow versioning.
	Version dispatcher.CommandID = 182
	// UpsertAttrs is the command ID for upserting search attributes.
	UpsertAttrs dispatcher.CommandID = 183
)

// SideEffectCmd carries a closure to be executed by the workflow dispatcher.
// The dispatcher will execute the function and record the result for replay.
type SideEffectCmd struct {
	Fn func() (any, error)
}

// CmdID implements dispatcher.Command.
func (c *SideEffectCmd) CmdID() dispatcher.CommandID { return SideEffect }

// Result from side effect execution.
type Result struct {
	Value any
	Error error
}

// CallCmd requests synchronous child workflow execution.
// The workflow will wait for the child to complete and return its result.
type CallCmd struct {
	ID      registry.ID      `json:"id"`
	Args    payload.Payloads `json:"args,omitempty"`
	Options *CallOptions     `json:"options,omitempty"`
}

// CmdID implements dispatcher.Command.
func (c *CallCmd) CmdID() dispatcher.CommandID { return Call }

// CallOptions configures child workflow execution.
type CallOptions struct {
	WorkflowID       string `json:"workflow_id,omitempty"`
	TaskQueue        string `json:"task_queue,omitempty"`
	ExecutionTimeout string `json:"execution_timeout,omitempty"`
	RunTimeout       string `json:"run_timeout,omitempty"`
	TaskTimeout      string `json:"task_timeout,omitempty"`
}

// CallResult is the result of a workflow.call operation.
type CallResult struct {
	Value payload.Payload
	Error error
}

// VersionCmd requests a version number for a code change.
type VersionCmd struct {
	ChangeID     string `json:"change_id"`
	MinSupported int    `json:"min_supported"`
	MaxSupported int    `json:"max_supported"`
}

// CmdID implements dispatcher.Command.
func (c *VersionCmd) CmdID() dispatcher.CommandID { return Version }

// VersionResult is the result of a workflow.version operation.
type VersionResult struct {
	Version int
}

// UpsertAttrsCmd requests updating workflow search attributes and/or memo.
// SearchAttrs are indexed metadata that enable workflow queries.
// Memo is arbitrary non-indexed data attached to the workflow.
type UpsertAttrsCmd struct {
	SearchAttrs map[string]any `json:"search,omitempty"`
	Memo        map[string]any `json:"memo,omitempty"`
}

// CmdID implements dispatcher.Command.
func (c *UpsertAttrsCmd) CmdID() dispatcher.CommandID { return UpsertAttrs }
