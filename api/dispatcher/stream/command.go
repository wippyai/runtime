// Package stream provides stream-related command types for the dispatcher system.
package stream

import (
	"github.com/wippyai/runtime/api/dispatcher"
)

// Command IDs for stream operations.
// Range 50-99 is reserved for stream commands.
const (
	CmdStreamRead  dispatcher.CommandID = 50 // Read chunk from stream
	CmdStreamClose dispatcher.CommandID = 51 // Close stream
)

// StreamReadCmd reads a chunk of data from a stream.
// Returns bytes via emit, or nil on EOF.
type StreamReadCmd struct {
	StreamID uint64
	Size     int64 // 0 = default chunk size
}

// CmdID implements dispatcher.Command.
func (c StreamReadCmd) CmdID() dispatcher.CommandID {
	return CmdStreamRead
}

// StreamCloseCmd closes a stream and releases resources.
type StreamCloseCmd struct {
	StreamID uint64
}

// CmdID implements dispatcher.Command.
func (c StreamCloseCmd) CmdID() dispatcher.CommandID {
	return CmdStreamClose
}
