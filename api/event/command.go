package event

import (
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/pid"
)

func init() {
	dispatcher.MustRegisterCommands("events",
		CmdEventsSubscribe, CmdEventsSend,
	)
}

// Command IDs for event bus operations.
// Range 90-94 is reserved for event bus commands.
const (
	CmdEventsSubscribe dispatcher.CommandID = 90 // Subscribe to event bus
	CmdEventsSend      dispatcher.CommandID = 91 // Send event to bus
)

// EventsSubscribeCmd subscribes to events from the bus and forwards them via relay.
type EventsSubscribeCmd struct {
	System string  // Event system pattern to subscribe to
	Kind   string  // Event kind pattern (optional)
	Topic  string  // Per-subscription topic for relay messages
	PID    pid.PID // Target process PID to send events to
}

// CmdID implements dispatcher.Command.
func (c EventsSubscribeCmd) CmdID() dispatcher.CommandID {
	return CmdEventsSubscribe
}

// EventsSendCmd sends an event to the bus.
type EventsSendCmd struct {
	System string // Event system
	Kind   string // Event kind
	Path   string // Event path
	Data   any    // Event data
}

// CmdID implements dispatcher.Command.
func (c EventsSendCmd) CmdID() dispatcher.CommandID {
	return CmdEventsSend
}

// Subscription represents an active event subscription.
type Subscription struct {
	System      string // Subscribed system pattern
	Kind        string // Subscribed kind pattern
	Topic       string // Relay topic for messages
	Unsubscribe func() // Cleanup function to unsubscribe
}
