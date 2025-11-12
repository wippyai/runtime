package security

import (
	"context"

	"github.com/ponyruntime/pony/api/security"
)

// todo: move to api
// WithSecurityConfig configures the security context based on the provided configuration
func WithSecurityConfig(ctx context.Context, config *security.Config) context.Context {
	if config == nil {
		return ctx
	}

	if err := security.SetActor(ctx, config.Actor); err != nil {
		return ctx
	}

	reg, ok := security.GetRegistry(ctx)
	if !ok {
		return ctx
	}

	allPolicies := make([]security.Policy, 0)

	for _, groupID := range config.PolicyGroups {
		groupScope, err := reg.GetPolicyGroup(groupID)
		if err == nil {
			allPolicies = append(allPolicies, groupScope.Policies()...)
		}
	}

	for _, policyID := range config.Policies {
		policy, err := reg.GetPolicy(policyID)
		if err == nil {
			allPolicies = append(allPolicies, policy)
		}
	}

	if len(allPolicies) > 0 {
		scope := NewScope(allPolicies)
		if err := security.SetScope(ctx, scope); err != nil {
			return ctx
		}
	}

	return ctx
}
