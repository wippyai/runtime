// Package poll provides poll-related command types for the dispatcher system.
package poll

import (
	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("poll", CmdPoll)
}

// Command IDs for poll operations.
// Range 70-79 is reserved for poll/async I/O commands.
const (
	CmdPoll dispatcher.CommandID = 70 // Wait for any pollable to become ready
)

// PollCmd waits for one or more pollables to become ready.
// Each pollable has a SourceID that links to a dispatcher resource (stream, timer, etc).
// Returns indices of ready pollables via emit.
type PollCmd struct {
	Pollables []uint64 // Source IDs (stream IDs, timer IDs, etc)
}

// CmdID implements dispatcher.Command.
func (c PollCmd) CmdID() dispatcher.CommandID {
	return CmdPoll
}

// PollResult contains the indices of ready pollables.
type PollResult struct {
	Ready []uint32 // Indices into original Pollables slice
}
