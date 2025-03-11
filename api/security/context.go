package security

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/registry"
)

// Context keys
var (
	actorCtx     = &ctxapi.Key{Name: "security.actor"}
	policySetCtx = &ctxapi.Key{Name: "security.policySet"}
	tokenCtx     = &ctxapi.Key{Name: "security.token"}
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

// WithPolicySet attaches a policy set to the context
func WithPolicySet(ctx context.Context, set PolicySet) context.Context {
	return context.WithValue(ctx, policySetCtx, set)
}

// GetPolicySet retrieves the policy set from the context
func GetPolicySet(ctx context.Context) (PolicySet, bool) {
	set, ok := ctx.Value(policySetCtx).(PolicySet)
	return set, ok
}

// WithPolicy creates a new context with an added policy
func WithPolicy(ctx context.Context, policy Policy) context.Context {
	set, ok := GetPolicySet(ctx)
	if !ok {
		set = NewEmptyPolicySet()
	}

	return WithPolicySet(ctx, set.With(policy))
}

// WithToken attaches token information to the context
func WithToken(ctx context.Context, token TokenInfo) context.Context {
	return context.WithValue(ctx, tokenCtx, token)
}

// GetToken retrieves token information from the context
func GetToken(ctx context.Context) (TokenInfo, bool) {
	token, ok := ctx.Value(tokenCtx).(TokenInfo)
	return token, ok
}

// IsAllowed checks if the current actor is allowed to perform an action
func IsAllowed(ctx context.Context, action, resource string, meta registry.Metadata) bool {
	actor, hasActor := GetActor(ctx)
	set, hasSet := GetPolicySet(ctx)

	if !hasActor || !hasSet {
		return false
	}

	result := set.Evaluate(actor, action, resource, meta)
	return result == Allow
}

// NewEmptyPolicySet creates an empty policy set.
func NewEmptyPolicySet() PolicySet {
	return nil // Implementation provided elsewhere
}
