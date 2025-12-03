// Package clock provides time-related command types for the dispatcher system.
package clock

import "github.com/wippyai/runtime/api/dispatcher"

// NowCmd requests current time from the clock service.
// Returns time.Time via emit(). For deterministic workflows (Temporal),
// handler returns workflow time instead of real time.
//
// Deprecated: This command should not be used. Time should be obtained directly
// without yielding through the dispatcher. This will be removed once WASM host
// is updated to return time synchronously.
type NowCmd struct{}

// CmdID implements dispatcher.Command.
func (c NowCmd) CmdID() dispatcher.CommandID {
	return CmdNow
}
