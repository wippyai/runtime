// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"
	"errors"
	"fmt"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/security"
)

// ResolveConfigPairs resolves config using the actor and scope already present
// in ctx. The returned pairs are a complete security context that can either be
// applied locally or transported to a new process frame.
//
// Resolution is atomic: if any declared policy or policy group cannot be fully
// resolved, no pairs are returned. Returning an actor or a partial scope beside
// an error would let a caller accidentally cross a security boundary with an
// incomplete configuration.
func ResolveConfigPairs(ctx context.Context, config *security.Config) ([]ctxapi.Pair, error) {
	if config == nil {
		return nil, nil
	}

	actor := config.Actor
	if !actorConfigured(actor) {
		if existing, ok := security.GetActor(ctx); ok {
			actor = existing
		}
	}
	pairs := []ctxapi.Pair{security.ActorPair(actor)}

	existingScope, hasExistingScope := security.GetScope(ctx)
	if len(config.PolicyGroups) == 0 && len(config.Policies) == 0 {
		if hasExistingScope && existingScope != nil {
			pairs = append(pairs, security.ScopePair(existingScope))
		}
		return pairs, nil
	}

	reg, ok := security.GetRegistry(ctx)
	if !ok {
		return nil, fmt.Errorf("security registry not available")
	}

	policies := make([]security.Policy, 0, len(config.PolicyGroups)+len(config.Policies))
	var resolutionErrors []error
	for _, groupID := range config.PolicyGroups {
		groupScope, err := reg.GetPolicyGroup(groupID)
		if err != nil {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("resolve security policy group %s: %w", groupID.String(), err))
			continue
		}
		if groupScope == nil {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("resolve security policy group %s: empty scope", groupID.String()))
			continue
		}
		groupPolicies := groupScope.Policies()
		if len(groupPolicies) == 0 {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("resolve security policy group %s: group contains no policies", groupID.String()))
			continue
		}
		for index, policy := range groupPolicies {
			if policy == nil {
				resolutionErrors = append(resolutionErrors, fmt.Errorf("resolve security policy group %s: policy %d is nil", groupID.String(), index))
				continue
			}
			policies = append(policies, policy)
		}
	}
	for _, policyID := range config.Policies {
		policy, err := reg.GetPolicy(policyID)
		if err != nil {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("resolve security policy %s: %w", policyID.String(), err))
			continue
		}
		if policy == nil {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("resolve security policy %s: policy is nil", policyID.String()))
			continue
		}
		policies = append(policies, policy)
	}
	if len(resolutionErrors) > 0 {
		return nil, errors.Join(resolutionErrors...)
	}
	if len(policies) == 0 {
		return nil, fmt.Errorf("security configuration resolved no policies")
	}

	scope := existingScope
	if scope == nil && len(policies) > 0 {
		scope = NewScope(policies)
	} else {
		for _, policy := range policies {
			scope = scope.With(policy)
		}
	}
	if scope != nil {
		pairs = append(pairs, security.ScopePair(scope))
	}

	return pairs, nil
}
