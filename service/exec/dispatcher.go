// SPDX-License-Identifier: MPL-2.0

// Package exec provides process execution command handlers for the dispatcher system.
package exec

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	execapi "github.com/wippyai/runtime/api/service/exec"
)

// exitCoder is an interface for errors that have an exit code.
type exitCoder interface {
	ExitCode() int
}

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
		err := waitCmd.Process.Wait()

		var exitCode int
		if err == nil {
			exitCode = 0
		} else if ec, ok := err.(exitCoder); ok {
			exitCode = ec.ExitCode()
			err = nil
		}

		if ctx.Err() == nil {
			receiver.CompleteYield(tag, execapi.ProcessWaitResponse{ExitCode: exitCode, Error: err}, nil)
		}
	}()

	return nil
}
