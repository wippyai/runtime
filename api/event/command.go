package event

import (
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/pid"
)

func init() {
	dispatcher.MustRegisterCommands("events",
		Subscribe, Send,
	)
}

// Command IDs for event bus operations.
// Range 90-94 is reserved for event bus commands.
const (
	Subscribe dispatcher.CommandID = 90 // Subscribe to event bus
	Send      dispatcher.CommandID = 91 // Send event to bus
)

// SubscribeCmd subscribes to events from the bus and forwards them via relay.
type SubscribeCmd struct {
	System string  // Event system pattern to subscribe to
	Kind   string  // Event kind pattern (optional)
	Topic  string  // Per-subscription topic for relay messages
	PID    pid.PID // Target process PID to send events to
}

// CmdID implements dispatcher.Command.
func (c SubscribeCmd) CmdID() dispatcher.CommandID {
	return Subscribe
}

// SendCmd sends an event to the bus.
type SendCmd struct {
	Data   any
	System string
	Kind   string
	Path   string
}

// CmdID implements dispatcher.Command.
func (c SendCmd) CmdID() dispatcher.CommandID {
	return Send
}

// Subscription represents an active event subscription.
type Subscription struct {
	Unsubscribe func()
	System      string
	Kind        string
	Topic       string
}
