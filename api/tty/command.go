// Package tty provides terminal I/O command types for the dispatcher system.
package tty

import "github.com/wippyai/runtime/api/dispatcher"

func init() {
	dispatcher.MustRegisterCommands("tty", Read, ReadLine, RawEnable, RawDisable)
}

// Command IDs for terminal I/O.
// Range 70-79 is reserved for terminal operations.
const (
	Read       dispatcher.CommandID = 70 // Read bytes from stdin
	ReadLine   dispatcher.CommandID = 71 // Read a line from stdin
	RawEnable  dispatcher.CommandID = 72 // Enable raw terminal mode
	RawDisable dispatcher.CommandID = 73 // Disable raw terminal mode
)

// DefaultReadSize is used when read size is not provided or <= 0.
const DefaultReadSize = 1024

// ReadCmd reads up to Size bytes from stdin.
type ReadCmd struct {
	Size int
}

// CmdID implements dispatcher.Command.
func (c ReadCmd) CmdID() dispatcher.CommandID {
	return Read
}

// ReadLineCmd reads a line from stdin.
type ReadLineCmd struct{}

// CmdID implements dispatcher.Command.
func (c ReadLineCmd) CmdID() dispatcher.CommandID {
	return ReadLine
}

// RawEnableCmd enables raw terminal mode.
type RawEnableCmd struct{}

// CmdID implements dispatcher.Command.
func (c RawEnableCmd) CmdID() dispatcher.CommandID {
	return RawEnable
}

// RawDisableCmd disables raw terminal mode.
type RawDisableCmd struct{}

// CmdID implements dispatcher.Command.
func (c RawDisableCmd) CmdID() dispatcher.CommandID {
	return RawDisable
}
