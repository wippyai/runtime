package actor

import (
	"fmt"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/relay"
)

// UnknownCommandError indicates no handler is registered for a command.
type UnknownCommandError struct {
	ID dispatcher.CommandID
}

func (e *UnknownCommandError) Error() string {
	return fmt.Sprintf("no handler registered for command %d", e.ID)
}

// ProcessNotIdleError indicates SendTo was called for a non-idle process.
type ProcessNotIdleError struct {
	ID uint64
}

func (e *ProcessNotIdleError) Error() string {
	return fmt.Sprintf("process %d is not idle", e.ID)
}

// ProcessNotFoundError indicates Send was called for an unknown PID.
type ProcessNotFoundError struct {
	PID relay.PID
}

func (e *ProcessNotFoundError) Error() string {
	return fmt.Sprintf("process %s not found", e.PID.String())
}

// ErrProcessClosed indicates the process queue rejected the message due to generation mismatch.
var ErrProcessClosed = fmt.Errorf("process closed")
