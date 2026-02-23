// SPDX-License-Identifier: MPL-2.0

// Package evalhost provides Lua evaluation command handlers.
package evalhost

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
)

// Dispatcher handles eval commands.
type Dispatcher struct {
	host *Host
}

// NewDispatcher creates an eval dispatcher.
func NewDispatcher(host *Host) *Dispatcher {
	return &Dispatcher{host: host}
}

// Start is a no-op for eval dispatcher.
func (d *Dispatcher) Start(_ context.Context) error {
	return nil
}

// Stop is a no-op for eval dispatcher.
func (d *Dispatcher) Stop(_ context.Context) error {
	return nil
}

// RegisterAll registers all eval handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(Compile, dispatcher.HandlerFunc(d.handleCompile))
	register(Run, dispatcher.HandlerFunc(d.handleRun))
}

func (d *Dispatcher) handleCompile(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	compileCmd := cmd.(CompileCmd)

	go func() {
		program, err := d.host.Compile(ctx, compileCmd)
		receiver.CompleteYield(tag, program, err)
	}()

	return nil
}

func (d *Dispatcher) handleRun(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	runCmd := cmd.(RunCmd)

	go func() {
		result, err := d.host.Run(ctx, runCmd)
		receiver.CompleteYield(tag, result, err)
	}()

	return nil
}
