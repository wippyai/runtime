// SPDX-License-Identifier: MPL-2.0

// Package exec provides process execution command handlers for the dispatcher system.
package exec

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	execapi "github.com/wippyai/runtime/api/service/exec"
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
	register(execapi.ProcessWait, dispatcher.HandlerFunc(d.handleProcessWait))
}

func (d *Dispatcher) handleProcessWait(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	waitCmd := cmd.(*execapi.ProcessWaitCmd)

	go func() {
		// WaitFor defers to a process that owns its own reap, so a child
		// already reaped through another path still reports its exit here.
		status := execapi.WaitFor(waitCmd.Process)

		if ctx.Err() == nil {
			receiver.CompleteYield(tag, execapi.ProcessWaitResponse{ExitCode: status.Code, Error: status.Err}, nil)
		}
	}()

	return nil
}
