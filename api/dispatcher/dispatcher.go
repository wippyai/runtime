// Package dispatcher re-exports command dispatch interfaces from api/process.
package dispatcher

import (
	"context"

	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
)

// Type aliases - use process directly in new code.
type (
	CommandID   = process.CommandID
	Command     = process.Command
	Completer   = process.Completer
	Handler     = process.Handler
	HandlerFunc = process.HandlerFunc
	Dispatcher  = process.Dispatcher
	Registry    = process.Registry
	Registrar   = process.Registrar
	Freezer     = process.Freezer
)

// Re-export constants
const KindHandler registry.Kind = process.KindHandler

// MustRegisterCommands delegates to process.
func MustRegisterCommands(module string, ids ...CommandID) {
	process.MustRegisterCommands(module, ids...)
}

// GetRegistry delegates to process.
func GetRegistry(ctx context.Context) Registry {
	return process.GetRegistry(ctx)
}

// GetRegistrar delegates to process.
func GetRegistrar(ctx context.Context) Registrar {
	return process.GetRegistrar(ctx)
}

// GetDispatcher delegates to process.
func GetDispatcher(ctx context.Context) Dispatcher {
	return process.GetDispatcher(ctx)
}

// WithRegistry delegates to process.
func WithRegistry(ctx context.Context, r Registry) error {
	return process.WithRegistry(ctx, r)
}
