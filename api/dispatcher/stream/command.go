// Package stream provides stream-related command types for the dispatcher system.
package stream

import (
	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("stream",
		CmdStreamRead, CmdStreamClose, CmdStreamWrite,
		CmdStreamSeek, CmdStreamFlush, CmdStreamStat,
	)
}

// Command IDs for stream operations.
// Range 50-59 is reserved for stream I/O commands.
const (
	CmdStreamRead  dispatcher.CommandID = 50 // Read chunk from stream
	CmdStreamClose dispatcher.CommandID = 51 // Close stream
	CmdStreamWrite dispatcher.CommandID = 52 // Write data to stream
	CmdStreamSeek  dispatcher.CommandID = 53 // Seek within stream
	CmdStreamFlush dispatcher.CommandID = 54 // Flush buffered data
	CmdStreamStat  dispatcher.CommandID = 55 // Get stream info (size, etc)
)

// Seek whence constants (matching io.Seek*)
const (
	SeekStart   = 0 // Seek relative to start
	SeekCurrent = 1 // Seek relative to current position
	SeekEnd     = 2 // Seek relative to end
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

// StreamWriteCmd writes data to a stream.
// Returns number of bytes written via emit.
type StreamWriteCmd struct {
	StreamID uint64
	Data     []byte
}

// CmdID implements dispatcher.Command.
func (c StreamWriteCmd) CmdID() dispatcher.CommandID {
	return CmdStreamWrite
}

// StreamSeekCmd seeks to a position in a seekable stream.
// Returns new position via emit.
type StreamSeekCmd struct {
	StreamID uint64
	Offset   int64
	Whence   int // SeekStart, SeekCurrent, SeekEnd
}

// CmdID implements dispatcher.Command.
func (c StreamSeekCmd) CmdID() dispatcher.CommandID {
	return CmdStreamSeek
}

// StreamFlushCmd flushes any buffered data.
type StreamFlushCmd struct {
	StreamID uint64
}

// CmdID implements dispatcher.Command.
func (c StreamFlushCmd) CmdID() dispatcher.CommandID {
	return CmdStreamFlush
}

// StreamStatCmd gets information about a stream.
// Returns StreamInfo via emit.
type StreamStatCmd struct {
	StreamID uint64
}

// CmdID implements dispatcher.Command.
func (c StreamStatCmd) CmdID() dispatcher.CommandID {
	return CmdStreamStat
}

// StreamInfo contains metadata about a stream.
type StreamInfo struct {
	Size     int64 // Total size (-1 if unknown)
	Position int64 // Current position (-1 if not seekable)
	Readable bool
	Writable bool
	Seekable bool
}
