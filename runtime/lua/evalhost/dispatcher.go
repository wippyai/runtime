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
	register(CmdCompile, dispatcher.HandlerFunc(d.handleCompile))
	register(CmdRun, dispatcher.HandlerFunc(d.handleRun))
}

func (d *Dispatcher) handleCompile(ctx context.Context, cmd dispatcher.Command, tag any, receiver dispatcher.ResultReceiver) error {
	compileCmd := cmd.(CompileCmd)

	go func() {
		program, err := d.host.Compile(ctx, compileCmd)
		if ctx.Err() != nil {
			receiver.CompleteYield(tag, nil, ctx.Err())
			return
		}
		if err != nil {
			receiver.CompleteYield(tag, nil, err)
			return
		}
		receiver.CompleteYield(tag, program, nil)
	}()

	return nil
}

func (d *Dispatcher) handleRun(_ context.Context, _ dispatcher.Command, tag any, receiver dispatcher.ResultReceiver) error {
	receiver.CompleteYield(tag, nil, nil)
	return nil
}
