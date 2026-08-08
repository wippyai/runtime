// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/security"
)

// ResolveConfigPairs resolves a declared security configuration into context
// pairs carrying the actor and its resolved policy scope. A launcher that
// starts a process outside any frame — the CLI terminal launcher starting a
// command entry — attaches the pairs to the process start context so the
// process runs under the entry-declared identity. Policy and group references
// resolve against the security registry in ctx; unresolvable references are
// skipped, matching WithSecurityConfig.
func ResolveConfigPairs(ctx context.Context, config *security.Config) []ctxapi.Pair {
	if config == nil {
		return nil
	}

	pairs := []ctxapi.Pair{security.ActorPair(config.Actor)}

	reg, ok := security.GetRegistry(ctx)
	if !ok {
		return pairs
	}

	policies := make([]security.Policy, 0, len(config.PolicyGroups)+len(config.Policies))
	for _, groupID := range config.PolicyGroups {
		if groupScope, err := reg.GetPolicyGroup(groupID); err == nil {
			policies = append(policies, groupScope.Policies()...)
		}
	}
	for _, policyID := range config.Policies {
		if policy, err := reg.GetPolicy(policyID); err == nil {
			policies = append(policies, policy)
		}
	}

	if len(policies) > 0 {
		pairs = append(pairs, security.ScopePair(NewScope(policies)))
	}

	return pairs
}
