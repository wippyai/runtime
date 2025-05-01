package security

import (
	"context"

	"github.com/ponyruntime/pony/api/security"
)

// WithSecurityConfig configures the security context based on the provided configuration
// If config is nil, the original context is returned unchanged
func WithSecurityConfig(ctx context.Context, config *security.Config) context.Context {
	if config == nil {
		return ctx
	}

	// Set the actor from the config
	ctx = security.WithActor(ctx, config.Actor)

	// Get the registry from the context
	reg, ok := security.GetRegistry(ctx)
	if !ok {
		// If no registry is available, we can't configure policies
		return ctx
	}

	// Collect all policies from groups and individual policy IDs
	allPolicies := make([]security.Policy, 0)

	// Add policies from groups
	for _, groupID := range config.PolicyGroups {
		groupScope, err := reg.GetPolicyGroup(groupID)
		if err == nil {
			// Add all policies from this group
			allPolicies = append(allPolicies, groupScope.Policies()...)
		}
	}

	// Add individual policies
	for _, policyID := range config.Policies {
		policy, err := reg.GetPolicy(policyID)
		if err == nil {
			allPolicies = append(allPolicies, policy)
		}
	}

	// Create a new scope with all collected policies
	if len(allPolicies) > 0 {
		scope := NewScope(allPolicies)
		ctx = security.WithScope(ctx, scope)
	}

	return ctx
}
