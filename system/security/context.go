// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"

	"github.com/wippyai/runtime/api/security"
)

func actorConfigured(actor security.Actor) bool {
	return actor.ID != "" || len(actor.Meta) > 0
}

// WithSecurityConfig configures the security context based on the provided configuration.
func WithSecurityConfig(ctx context.Context, config *security.Config) context.Context {
	if config == nil {
		return ctx
	}

	if actorConfigured(config.Actor) {
		if err := security.SetActor(ctx, config.Actor); err != nil {
			return ctx
		}
	} else if _, ok := security.GetActor(ctx); !ok {
		if err := security.SetActor(ctx, config.Actor); err != nil {
			return ctx
		}
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
		if existing, ok := security.GetScope(ctx); ok && existing != nil {
			scope = existing
			for _, policy := range allPolicies {
				scope = scope.With(policy)
			}
		}
		if err := security.SetScope(ctx, scope); err != nil {
			return ctx
		}
	}

	return ctx
}
