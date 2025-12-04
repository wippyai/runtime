// Package exec provides process execution command handlers for the dispatcher system.
package exec

import (
	"context"
	"errors"
	osexec "os/exec"

	"github.com/wippyai/runtime/api/dispatcher"
	execapi "github.com/wippyai/runtime/api/dispatcher/exec"
)

// Dispatcher handles exec commands.
type Dispatcher struct{}

// NewDispatcher creates a new exec dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{}
}

// Start is a no-op for exec dispatcher.
func (d *Dispatcher) Start(_ context.Context) error {
	return nil
}

// Stop is a no-op for exec dispatcher.
func (d *Dispatcher) Stop(_ context.Context) error {
	return nil
}

// RegisterAll registers all exec handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(execapi.CmdProcessWait, dispatcher.HandlerFunc(d.handleProcessWait))
}

func (d *Dispatcher) handleProcessWait(ctx context.Context, cmd dispatcher.Command, complete dispatcher.Completer) error {
	waitCmd := cmd.(*execapi.ProcessWaitCmd)

	go func() {
		err := waitCmd.Process.Wait()

		var exitCode int
		if err == nil {
			exitCode = 0
		} else {
			var exitErr *osexec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
				err = nil
			}
		}

		if ctx.Err() == nil {
			complete.Complete(execapi.ProcessWaitResponse{ExitCode: exitCode, Error: err}, nil)
		}
	}()

	return nil
}
