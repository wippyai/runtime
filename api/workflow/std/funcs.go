package std

import "github.com/wippyai/runtime/api/registry"

// FuncsCallHeader is the header payload for funcs.call commands.
// This is serialized as Params[0], with function arguments in Params[1:].
type FuncsCallHeader struct {
	// Target is the function registry ID to invoke.
	Target registry.ID `json:"target"`

	// Context contains application context values to pass to the function.
	Context map[string]any `json:"context,omitempty"`

	// Security contains the security context for this call.
	Security *SecurityContext `json:"security,omitempty"`

	// Options contains execution options for the function call.
	Options *FuncsCallOptions `json:"options,omitempty"`
}

// FuncsCallOptions defines execution options for function calls.
type FuncsCallOptions struct {
	// Timeout as duration string (e.g., "30s", "5m").
	// Maps to Temporal ScheduleToCloseTimeout.
	Timeout string `json:"timeout,omitempty"`

	// StartToCloseTimeout as duration string.
	// Maps to Temporal StartToCloseTimeout.
	StartToCloseTimeout string `json:"start_to_close_timeout,omitempty"`

	// ScheduleToStartTimeout as duration string.
	// Maps to Temporal ScheduleToStartTimeout.
	ScheduleToStartTimeout string `json:"schedule_to_start_timeout,omitempty"`

	// HeartbeatTimeout as duration string.
	// Maps to Temporal HeartbeatTimeout.
	HeartbeatTimeout string `json:"heartbeat_timeout,omitempty"`

	// Retry defines retry policy for the function call.
	Retry *RetryPolicy `json:"retry,omitempty"`

	// TaskQueue for routing (Temporal-specific, optional).
	TaskQueue string `json:"task_queue,omitempty"`

	// WaitForCancellation indicates whether to wait for activity cancellation.
	WaitForCancellation bool `json:"wait_for_cancellation,omitempty"`
}
