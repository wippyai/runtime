package security

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

var (
	actorKey    = &ctxapi.Key{Name: "security.actor", Inherit: true}
	scopeKey    = &ctxapi.Key{Name: "security.scope", Inherit: true}
	registryKey = &ctxapi.Key{Name: "security.registry"}
	strictKey   = &ctxapi.Key{Name: "security.strict"}
)

// ActorPair creates a context.Pair for setting an actor.
func ActorPair(actor Actor) ctxapi.Pair {
	return ctxapi.Pair{Key: actorKey, Value: actor}
}

// ScopePair creates a context.Pair for setting a scope.
func ScopePair(scope Scope) ctxapi.Pair {
	return ctxapi.Pair{Key: scopeKey, Value: scope}
}

// SetActor sets the actor in the FrameContext.
func SetActor(ctx context.Context, actor Actor) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ErrNoFrameContext
	}
	return fc.Set(actorKey, actor)
}

// GetActor retrieves the actor from the FrameContext.
func GetActor(ctx context.Context) (Actor, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return Actor{}, false
	}

	if val, ok := fc.Get(actorKey); ok {
		if actor, ok := val.(Actor); ok {
			return actor, true
		}
	}
	return Actor{}, false
}

// SetScope sets the scope in the FrameContext.
func SetScope(ctx context.Context, scope Scope) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ErrNoFrameContext
	}
	return fc.Set(scopeKey, scope)
}

// GetScope retrieves the scope from the FrameContext.
func GetScope(ctx context.Context) (Scope, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, false
	}
	if val, ok := fc.Get(scopeKey); ok {
		if scope, ok := val.(Scope); ok {
			return scope, true
		}
	}

	return nil, false
}

// WithPolicy adds a policy to the current scope in the FrameContext.
func WithPolicy(ctx context.Context, policy Policy) error {
	scope, ok := GetScope(ctx)
	if !ok {
		return ErrScopeNotFound
	}
	return SetScope(ctx, scope.With(policy))
}

// WithRegistry attaches a security registry to the context.
func WithRegistry(ctx context.Context, reg Registry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryKey) == nil {
		ac.With(registryKey, reg)
	}
	return ctx
}

// GetRegistry retrieves the security registry from the context.
func GetRegistry(ctx context.Context) (Registry, bool) {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil, false
	}
	if val := ac.Get(registryKey); val != nil {
		if reg, ok := val.(Registry); ok {
			return reg, true
		}
	}
	return nil, false
}

// GetPolicy retrieves a policy by ID using the registry from context.
func GetPolicy(ctx context.Context, id registry.ID) (Policy, error) {
	reg, ok := GetRegistry(ctx)
	if !ok {
		return nil, ErrRegistryNotFound
	}
	return reg.GetPolicy(id)
}

// GetPolicyGroup retrieves a policy group by ID using the registry from context.
func GetPolicyGroup(ctx context.Context, id registry.ID) (Scope, error) {
	reg, ok := GetRegistry(ctx)
	if !ok {
		return nil, ErrRegistryNotFound
	}
	return reg.GetPolicyGroup(id)
}

// IsAllowed checks if the current actor is allowed to perform an action.
func IsAllowed(ctx context.Context, action, resource string, meta attrs.Bag) bool {
	actor, hasActor := GetActor(ctx)
	scope, hasScope := GetScope(ctx)

	if !hasActor || !hasScope {
		return false
	}

	result := scope.Evaluate(actor, action, resource, meta)
	return result == Allow
}

// SetStrictMode sets the security strict mode in the AppContext.
// When strict mode is enabled, incomplete security contexts will deny access.
// Must be called during boot before AppContext is sealed.
func SetStrictMode(ctx context.Context, strict bool) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	ac.With(strictKey, strict)
	return ctx
}

// IsStrictMode checks if security strict mode is enabled.
// Returns true (strict) by default if not explicitly set.
func IsStrictMode(ctx context.Context) bool {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return true
	}
	if val := ac.Get(strictKey); val != nil {
		if strict, ok := val.(bool); ok {
			return strict
		}
	}
	return true
}
