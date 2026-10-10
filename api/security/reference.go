// SPDX-License-Identifier: MPL-2.0

package security

import "github.com/wippyai/runtime/api/registry"

// PolicyReferenceID returns an ID that can reconstruct the policy's authority.
// Policies with additional execution-local constraints may implement ReferenceID
// to reject ID-only storage or propagation. Their ordinary ID remains unchanged.
func PolicyReferenceID(policy Policy) (registry.ID, error) {
	if policy == nil {
		return registry.ID{}, ErrPolicyNotFound
	}
	if reference, ok := policy.(interface {
		ReferenceID() (registry.ID, error)
	}); ok {
		return reference.ReferenceID()
	}
	return policy.ID(), nil
}

// PolicyReferenceIDs returns reconstructable policy IDs for a scope. A scope
// may implement ReferenceIDs to reject ID-only transport even when it is empty.
// The returned IDs are atomic: a failure never exposes a partial authority.
func PolicyReferenceIDs(scope Scope) ([]registry.ID, error) {
	if scope == nil {
		return nil, nil
	}
	if reference, ok := scope.(interface {
		ReferenceIDs() ([]registry.ID, error)
	}); ok {
		return reference.ReferenceIDs()
	}
	policies := scope.Policies()
	ids := make([]registry.ID, 0, len(policies))
	for _, policy := range policies {
		id, err := PolicyReferenceID(policy)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
