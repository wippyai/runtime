// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"context"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology/namereg/global"
)

// ContextPIDRegistry preserves lookup cancellation and errors across name scopes.
// A composed implementation may resolve an available weaker scope after a
// stronger scope fails, but must return the failure if no scope resolves.
// PIDRegistry remains supported for existing local registry implementations.
type ContextPIDRegistry interface {
	PIDRegistry
	LookupContext(context.Context, string) (pid.PID, bool, error)
}

// LocalPIDRegistry exposes the node-local name table without composed lookup or
// parent fallback. Exact LOCAL lookup requires this capability; plain Lookup
// cannot establish which namespace supplied a binding.
type LocalPIDRegistry interface {
	PIDRegistry
	LookupLocal(string) (pid.PID, bool)
}

// LookupScopedPID selects exactly one namespace. Missing registries and lookup
// failures never fall through to another scope. CONSISTENT and STRONG select the
// same global ownership namespace: their registration protocols differ, but
// neither selector strengthens the backend's read consistency or freshness.
func LookupScopedPID(ctx context.Context, name string, scope RegistrationMode) (pid.PID, bool, error) {
	if err := ctx.Err(); err != nil {
		return pid.PID{}, false, err
	}
	var p pid.PID
	var found bool
	var err error
	switch scope {
	case Local:
		registry, ok := GetRegistry(ctx).(LocalPIDRegistry)
		if !ok {
			return pid.PID{}, false, ErrNameRegistryUnavailable
		}
		p, found = registry.LookupLocal(name)
	case Eventual:
		registry := GetEventualRegistry(ctx)
		if registry == nil {
			return pid.PID{}, false, ErrNameRegistryUnavailable
		}
		var result global.LookupResult
		result, err = registry.Lookup(ctx, name)
		p, found = result.PID, result.Found
	case Consistent, Strong:
		registry := global.GetRegistry(ctx)
		if registry == nil {
			return pid.PID{}, false, ErrNameRegistryUnavailable
		}
		var result global.LookupResult
		result, err = registry.Lookup(ctx, name)
		p, found = result.PID, result.Found
	default:
		return pid.PID{}, false, ErrInvalidNameScope
	}
	if ctx.Err() != nil {
		return pid.PID{}, false, ctx.Err()
	}
	if err != nil {
		return pid.PID{}, false, err
	}
	return p, found, nil
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
