// Package stream provides stream-related command types for the dispatcher system.
package stream

import (
	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("stream",
		CmdRead, CmdClose, CmdWrite,
		CmdSeek, CmdFlush, CmdStat,
		CmdScannerCreate, CmdScannerScan,
	)
}

// Command IDs for stream operations.
// Range 50-59 is reserved for stream I/O commands.
const (
	CmdRead          dispatcher.CommandID = 50 // Read chunk from stream
	CmdClose         dispatcher.CommandID = 51 // Close stream
	CmdWrite         dispatcher.CommandID = 52 // Write data to stream
	CmdSeek          dispatcher.CommandID = 53 // Seek within stream
	CmdFlush         dispatcher.CommandID = 54 // Flush buffered data
	CmdStat          dispatcher.CommandID = 55 // Get stream info (size, etc)
	CmdScannerCreate dispatcher.CommandID = 56 // Create scanner from stream
	CmdScannerScan   dispatcher.CommandID = 57 // Scan next token
)

// Seek whence constants (matching io.Seek*)
const (
	SeekStart   = 0 // Seek relative to start
	SeekCurrent = 1 // Seek relative to current position
	SeekEnd     = 2 // Seek relative to end
)

// ReadCmd reads a chunk of data from a stream.
// Returns bytes via emit, or nil on EOF.
type ReadCmd struct {
	StreamID uint64
	Size     int64 // 0 = default chunk size
}

// CmdID implements dispatcher.Command.
func (c ReadCmd) CmdID() dispatcher.CommandID {
	return CmdRead
}

// CloseCmd closes a stream and releases resources.
type CloseCmd struct {
	StreamID uint64
}

// CmdID implements dispatcher.Command.
func (c CloseCmd) CmdID() dispatcher.CommandID {
	return CmdClose
}

// WriteCmd writes data to a stream.
// Returns number of bytes written via emit.
type WriteCmd struct {
	StreamID uint64
	Data     []byte
}

// CmdID implements dispatcher.Command.
func (c WriteCmd) CmdID() dispatcher.CommandID {
	return CmdWrite
}

// SeekCmd seeks to a position in a seekable stream.
// Returns new position via emit.
type SeekCmd struct {
	StreamID uint64
	Offset   int64
	Whence   int // SeekStart, SeekCurrent, SeekEnd
}

// CmdID implements dispatcher.Command.
func (c SeekCmd) CmdID() dispatcher.CommandID {
	return CmdSeek
}

// FlushCmd flushes any buffered data.
type FlushCmd struct {
	StreamID uint64
}

// CmdID implements dispatcher.Command.
func (c FlushCmd) CmdID() dispatcher.CommandID {
	return CmdFlush
}

// StatCmd gets information about a stream.
// Returns Info via emit.
type StatCmd struct {
	StreamID uint64
}

// CmdID implements dispatcher.Command.
func (c StatCmd) CmdID() dispatcher.CommandID {
	return CmdStat
}

// Info contains metadata about a stream.
type Info struct {
	Size     int64 // Total size (-1 if unknown)
	Position int64 // Current position (-1 if not seekable)
	Readable bool
	Writable bool
	Seekable bool
}

// Scanner split type constants.
const (
	SplitLines = 0 // Split on newlines (default)
	SplitWords = 1 // Split on whitespace
	SplitBytes = 2 // Split on individual bytes
	SplitRunes = 3 // Split on UTF-8 runes
)

// ScannerCreateCmd creates a scanner from a stream.
type ScannerCreateCmd struct {
	StreamID  uint64
	SplitType int
}

func (c ScannerCreateCmd) CmdID() dispatcher.CommandID {
	return CmdScannerCreate
}

// ScannerScanCmd advances scanner to next token.
type ScannerScanCmd struct {
	ScannerID uint64
}

func (c ScannerScanCmd) CmdID() dispatcher.CommandID {
	return CmdScannerScan
}

// ScanResult contains the result of a scan operation.
type ScanResult struct {
	HasToken bool
	Text     string
	Error    string
}
