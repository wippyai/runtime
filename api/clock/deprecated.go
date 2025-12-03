// Package clock provides time-related command types for the dispatcher system.
package clock

import (
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
)

// NowCmd requests current time from the clock service.
//
// Deprecated: Time should be obtained directly without yielding through the dispatcher.
// Will be removed once WASM host is updated to return time synchronously.
type NowCmd struct{}

// CmdID implements dispatcher.Command.
func (c NowCmd) CmdID() dispatcher.CommandID {
	return CmdNow
}

// AfterCmd creates a channel that receives a value after duration.
//
// Deprecated: Use TimerStartCmd/TimerWaitCmd instead. AfterCmd is semantically
// identical to timer but is incorrectly coupled to Lua implementation.
type AfterCmd struct {
	Duration time.Duration
}

// CmdID implements dispatcher.Command.
func (c AfterCmd) CmdID() dispatcher.CommandID {
	return CmdAfter
}
