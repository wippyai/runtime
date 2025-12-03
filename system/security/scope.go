package security

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
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

	for _, policy := range policies {
		scope.policies[policy.ID()] = policy
	}

	return scope
}

func (s *scopeImpl) With(policy security.Policy) security.Scope {
	newScope := &scopeImpl{
		policies: make(map[registry.ID]security.Policy, len(s.policies)+1),
	}

	for id, p := range s.policies {
		newScope.policies[id] = p
	}

	newScope.policies[policy.ID()] = policy

	return newScope
}

func (s *scopeImpl) Without(policyID registry.ID) security.Scope {
	if _, exists := s.policies[policyID]; !exists {
		return s
	}

	newScope := &scopeImpl{
		policies: make(map[registry.ID]security.Policy, len(s.policies)-1),
	}

	for id, policy := range s.policies {
		if id != policyID {
			newScope.policies[id] = policy
		}
	}

	return newScope
}

func (s *scopeImpl) Evaluate(actor security.Actor, action, resource string, meta attrs.Bag) security.Result {
	if len(s.policies) == 0 {
		return security.Undefined
	}

	foundDecision := false
	var lastResult security.Result

	for _, policy := range s.policies {
		result := policy.Evaluate(actor, action, resource, meta)

		if result == security.Undefined {
			continue
		}

		foundDecision = true
		lastResult = result

		if result == security.Deny {
			return security.Deny
		}
	}

	if !foundDecision {
		return security.Undefined
	}

	return lastResult
}

func (s *scopeImpl) Contains(policyID registry.ID) bool {
	_, exists := s.policies[policyID]
	return exists
}

func (s *scopeImpl) Policies() []security.Policy {
	policies := make([]security.Policy, 0, len(s.policies))
	for _, policy := range s.policies {
		policies = append(policies, policy)
	}
	return policies
}

var _ security.Scope = (*scopeImpl)(nil)
