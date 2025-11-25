package std

import "github.com/wippyai/runtime/api/payload"

// Task represents an inbound request from host to workflow.
// The workflow processes it and calls Complete() or Fail().
//
// This is the reverse of Command:
//   - Command (outbound): Workflow creates → Host executes → Host calls Complete()
//   - Task (inbound): Host creates → Workflow receives → Workflow calls Complete()/Fail()
type Task interface {
	// Type returns driver-specific task type.
	// Examples: "view", "update" (btea), "query", "update.validate" (Temporal)
	Type() string

	// Input returns the task input payloads.
	Input() payload.Payloads

	// Complete marks task done with result.
	Complete(value payload.Payload) error

	// Fail marks task failed with error.
	Fail(err error) error
}
