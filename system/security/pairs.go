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
// Resolution is best-effort: valid pairs are returned together with errors for
// unresolved policy references. Existing callers that historically tolerated
// missing references can apply the pairs and ignore the error; trust boundaries
// such as process launchers must reject it.
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
		if hasExistingScope && existingScope != nil {
			pairs = append(pairs, security.ScopePair(existingScope))
		}
		return pairs, fmt.Errorf("security registry not available")
	}

	policies := make([]security.Policy, 0, len(config.PolicyGroups)+len(config.Policies))
	var resolutionErrors []error
	for _, groupID := range config.PolicyGroups {
		groupScope, err := reg.GetPolicyGroup(groupID)
		if err != nil {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("resolve security policy group %s: %w", groupID.String(), err))
			continue
		}
		policies = append(policies, groupScope.Policies()...)
	}
	for _, policyID := range config.Policies {
		policy, err := reg.GetPolicy(policyID)
		if err != nil {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("resolve security policy %s: %w", policyID.String(), err))
			continue
		}
		policies = append(policies, policy)
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

	return pairs, errors.Join(resolutionErrors...)
}
