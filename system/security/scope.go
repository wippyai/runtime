package security

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/security"
)

// scopeImpl implements the Scope interface to provide an immutable
// collection of security policies
type scopeImpl struct {
	policies map[registry.ID]security.Policy
}

// NewScope creates a new Scope with the given policies
func NewScope(policies []security.Policy) security.Scope {
	scope := &scopeImpl{
		policies: make(map[registry.ID]security.Policy),
	}

	if policies != nil {
		for _, policy := range policies {
			scope.policies[policy.ID()] = policy
		}
	}

	return scope
}

// With returns a new Scope with the added policy
func (s *scopeImpl) With(policy security.Policy) security.Scope {
	// Create a new scope with copied policies map
	newScope := &scopeImpl{
		policies: make(map[registry.ID]security.Policy, len(s.policies)+1),
	}

	// Copy existing policies
	for id, p := range s.policies {
		newScope.policies[id] = p
	}

	// Add the new policy
	newScope.policies[policy.ID()] = policy

	return newScope
}

// Without returns a new Scope without the specified policy
func (s *scopeImpl) Without(policyID registry.ID) security.Scope {
	// If policy doesn't exist, return the same scope
	if _, exists := s.policies[policyID]; !exists {
		return s
	}

	// Create a new scope with copied policies map
	newScope := &scopeImpl{
		policies: make(map[registry.ID]security.Policy, len(s.policies)-1),
	}

	// Copy existing policies except the one to remove
	for id, policy := range s.policies {
		if id != policyID {
			newScope.policies[id] = policy
		}
	}

	return newScope
}

// Evaluate checks all policies and determines if action is allowed
func (s *scopeImpl) Evaluate(actor security.Actor, action, resource string, meta registry.Metadata) security.Result {
	if len(s.policies) == 0 {
		return security.Undefined
	}

	// Track if we've seen at least one policy decision
	foundDecision := false
	var lastResult security.Result

	// Apply all policies in scope
	for _, policy := range s.policies {
		result := policy.Evaluate(actor, action, resource, meta)

		// Skip undefined results
		if result == security.Undefined {
			continue
		}

		// Track that we found a decision
		foundDecision = true
		lastResult = result

		// Deny takes precedence - if any policy denies, the action is denied
		if result == security.Deny {
			return security.Deny
		}
	}

	// If no policy made a decision, return Undefined
	if !foundDecision {
		return security.Undefined
	}

	// Otherwise return the last meaningful result (which must be Allow since we would
	// have returned early for Deny)
	return lastResult
}

// Contains checks if a policy is in the scope
func (s *scopeImpl) Contains(policyID registry.ID) bool {
	_, exists := s.policies[policyID]
	return exists
}

// Policies returns all policies in the scope
func (s *scopeImpl) Policies() []security.Policy {
	policies := make([]security.Policy, 0, len(s.policies))
	for _, policy := range s.policies {
		policies = append(policies, policy)
	}
	return policies
}

// Ensure scopeImpl implements the Scope interface
var _ security.Scope = (*scopeImpl)(nil)
