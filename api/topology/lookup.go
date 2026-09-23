// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"context"

	"github.com/wippyai/runtime/api/pid"
)

// ContextPIDRegistry preserves lookup cancellation and errors across name scopes.
// A composed implementation may resolve an available weaker scope after a
// stronger scope fails, but must return the failure if no scope resolves.
// PIDRegistry remains supported for existing local registry implementations.
type ContextPIDRegistry interface {
	PIDRegistry
	LookupContext(context.Context, string) (pid.PID, bool, error)
}

// LookupPID uses context-aware lookup when available. The legacy fallback cannot
// interrupt a blocking implementation or recover errors hidden by its bool API;
// implementations that perform remote I/O must implement ContextPIDRegistry.
func LookupPID(ctx context.Context, registry PIDRegistry, name string) (pid.PID, bool, error) {
	if err := ctx.Err(); err != nil {
		return pid.PID{}, false, err
	}
	if registry == nil {
		return pid.PID{}, false, nil
	}
	if contextual, ok := registry.(ContextPIDRegistry); ok {
		return contextual.LookupContext(ctx, name)
	}
	p, found := registry.Lookup(name)
	if err := ctx.Err(); err != nil {
		return pid.PID{}, false, err
	}
	return p, found, nil
}
