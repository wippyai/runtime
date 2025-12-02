// Package dispatcher re-exports command dispatch interfaces from api/process2.
package dispatcher

import (
	"context"

	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/registry"
)

// Type aliases - use process2 directly in new code.
type (
	CommandID      = process2.CommandID
	Command        = process2.Command
	Emitter        = process2.Emitter
	Handler        = process2.Handler
	HandlerFunc    = process2.HandlerFunc
	Callable       = process2.Callable
	Dispatcher     = process2.Dispatcher
	AsyncScheduler = process2.AsyncScheduler
	Registry       = process2.Registry
	Registrar      = process2.Registrar
	Freezer        = process2.Freezer
)

// Re-export constants
const KindHandler registry.Kind = process2.KindHandler

// MustRegisterCommands delegates to process2.
func MustRegisterCommands(module string, ids ...CommandID) {
	process2.MustRegisterCommands(module, ids...)
}

// GetRegistry delegates to process2.
func GetRegistry(ctx context.Context) Registry {
	return process2.GetRegistry(ctx)
}

// GetRegistrar delegates to process2.
func GetRegistrar(ctx context.Context) Registrar {
	return process2.GetRegistrar(ctx)
}

// GetDispatcher delegates to process2.
func GetDispatcher(ctx context.Context) Dispatcher {
	return process2.GetDispatcher(ctx)
}

// WithRegistry delegates to process2.
func WithRegistry(ctx context.Context, r Registry) error {
	return process2.WithRegistry(ctx, r)
}
