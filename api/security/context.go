package security

import (
	"context"
	"errors"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/registry"
)

// Context keys
var (
	actorCtx = &ctxapi.Key{Name: "security.actor"}

	scopeCtx = &ctxapi.Key{Name: "security.scope"}

	registryCtx = &ctxapi.Key{Name: "security.registry"}
)

// WithActor attaches an actor to the context
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorCtx, actor)
}

// GetActor retrieves the actor from the context
func GetActor(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorCtx).(Actor)
	return actor, ok
}

// WithScope attaches a scope to the context
func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeCtx, scope)
}

// GetScope retrieves the scope from the context
func GetScope(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(scopeCtx).(Scope)
	return scope, ok
}

// WithPolicy creates a new context with an added policy
func WithPolicy(ctx context.Context, policy Policy) context.Context {
	scope, ok := GetScope(ctx)
	if !ok {
		panic("security scope not found in context")
	}

	return WithScope(ctx, scope.With(policy))
}

// WithRegistry attaches a security registry to the context
func WithRegistry(ctx context.Context, registry Registry) context.Context {
	return context.WithValue(ctx, registryCtx, registry)
}

// GetRegistry retrieves the security registry from the context
func GetRegistry(ctx context.Context) (Registry, bool) {
	reg, ok := ctx.Value(registryCtx).(Registry)
	return reg, ok
}

// GetPolicy retrieves a policy by ID using the registry from context
func GetPolicy(ctx context.Context, id registry.ID) (Policy, error) {
	reg, ok := GetRegistry(ctx)
	if !ok {
		return nil, errors.New("security registry not found in context")
	}
	return reg.GetPolicy(id)
}

// GetPolicyGroup retrieves a policy group by ID using the registry from context
func GetPolicyGroup(ctx context.Context, id registry.ID) (Scope, error) {
	reg, ok := GetRegistry(ctx)
	if !ok {
		return nil, errors.New("security registry not found in context")
	}
	return reg.GetPolicyGroup(id)
}

// IsAllowed checks if the current actor is allowed to perform an action
func IsAllowed(ctx context.Context, action, resource string, meta registry.Metadata) bool {
	actor, hasActor := GetActor(ctx)
	scope, hasScope := GetScope(ctx)

	if !hasActor || !hasScope {
		return false
	}

	result := scope.Evaluate(actor, action, resource, meta)
	return result == Allow
}

func CopyContext(source, target context.Context) context.Context {
	// Copy the actor
	if actor, ok := GetActor(source); ok {
		target = WithActor(target, actor)
	}

	// Copy the scope
	if scope, ok := GetScope(source); ok {
		target = WithScope(target, scope)
	}

	return target
}
