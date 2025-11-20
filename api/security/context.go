// Package security provides security and authentication abstractions.
package security

import (
	"context"
	"errors"
	"runtime/debug"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// Context keys
var (
	actorCtx = &ctxapi.Key{Name: "security.actor", Inherit: true}

	scopeCtx = &ctxapi.Key{Name: "security.scope", Inherit: true}

	registryCtx = &ctxapi.Key{Name: "security.registry"}
)

// ActorPair creates a context.Pair for setting an actor.
// Use this to build context override pairs for Task/Launch.
func ActorPair(actor Actor) ctxapi.Pair {
	return ctxapi.Pair{Key: actorCtx, Value: actor}
}

// ScopePair creates a context.Pair for setting a scope.
// Use this to build context override pairs for Task/Launch.
func ScopePair(scope Scope) ctxapi.Pair {
	return ctxapi.Pair{Key: scopeCtx, Value: scope}
}

// SetActor sets the actor in the FrameContext.
// Returns error if no frame context or frame is sealed.
func SetActor(ctx context.Context, actor Actor) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return errors.New("no frame context available")
	}

	err := fc.Set(actorCtx, actor)
	if err != nil {
		// TODO: REMOVE
		logger := logs.GetLogger(ctx)
		if logger != nil {
			logger.Error("SetActor failed - frame is sealed",
				zap.String("actor_id", actor.ID),
				zap.Bool("frame_sealed", fc.IsSealed()),
				zap.Error(err),
				zap.String("stack_trace", string(debug.Stack())))

			// Log parent chain
			parent := fc.Parent()
			depth := 0
			for parent != nil && depth < 5 {
				logger.Debug("SetActor - parent frame chain",
					zap.Int("depth", depth),
					zap.Bool("parent_sealed", parent.IsSealed()))
				parent = parent.Parent()
				depth++
			}
		}
	}

	return err
}

// GetActor retrieves the actor from the FrameContext.
func GetActor(ctx context.Context) (Actor, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return Actor{}, false
	}
	if val, ok := fc.Get(actorCtx); ok {
		if actor, ok := val.(Actor); ok {
			return actor, true
		}
	}
	return Actor{}, false
}

// SetScope sets the scope in the FrameContext.
// Returns error if no frame context or frame is sealed.
func SetScope(ctx context.Context, scope Scope) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return errors.New("no frame context available")
	}

	err := fc.Set(scopeCtx, scope)
	if err != nil {
		// TODO: REMOVE
		logger := logs.GetLogger(ctx)
		if logger != nil {
			policyCount := 0
			if scope != nil {
				policyCount = len(scope.Policies())
			}

			logger.Error("SetScope failed - frame is sealed",
				zap.Int("policies", policyCount),
				zap.Bool("frame_sealed", fc.IsSealed()),
				zap.Error(err),
				zap.String("stack_trace", string(debug.Stack())))

			// Log parent chain
			parent := fc.Parent()
			depth := 0
			for parent != nil && depth < 5 {
				logger.Debug("SetScope - parent frame chain",
					zap.Int("depth", depth),
					zap.Bool("parent_sealed", parent.IsSealed()))
				parent = parent.Parent()
				depth++
			}
		}
	}

	return err
}

// GetScope retrieves the scope from the FrameContext.
func GetScope(ctx context.Context) (Scope, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, false
	}
	if val, ok := fc.Get(scopeCtx); ok {
		if scope, ok := val.(Scope); ok {
			return scope, true
		}
	}
	return nil, false
}

// WithPolicy adds a policy to the current scope in the FrameContext.
// Returns error if no scope found or frame is sealed.
func WithPolicy(ctx context.Context, policy Policy) error {
	scope, ok := GetScope(ctx)
	if !ok {
		return errors.New("security scope not found in context")
	}

	return SetScope(ctx, scope.With(policy))
}

// WithRegistry attaches a security registry to the context
func WithRegistry(ctx context.Context, registry Registry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryCtx) == nil {
		ac.With(registryCtx, registry)
	}
	return ctx
}

// GetRegistry retrieves the security registry from the context
func GetRegistry(ctx context.Context) (Registry, bool) {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil, false
	}
	if val := ac.Get(registryCtx); val != nil {
		if reg, ok := val.(Registry); ok {
			return reg, true
		}
	}
	return nil, false
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
