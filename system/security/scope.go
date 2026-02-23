// SPDX-License-Identifier: MPL-2.0

package security

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
)

type scope struct {
	policies map[registry.ID]security.Policy
}

// NewScope creates a new Scope with the given policies.
func NewScope(policies []security.Policy) security.Scope {
	s := &scope{
		policies: make(map[registry.ID]security.Policy),
	}
	for _, policy := range policies {
		s.policies[policy.ID()] = policy
	}
	return s
}

func (s *scope) With(policy security.Policy) security.Scope {
	newScope := &scope{
		policies: make(map[registry.ID]security.Policy, len(s.policies)+1),
	}
	for id, p := range s.policies {
		newScope.policies[id] = p
	}
	newScope.policies[policy.ID()] = policy
	return newScope
}

func (s *scope) Without(policyID registry.ID) security.Scope {
	if _, exists := s.policies[policyID]; !exists {
		return s
	}
	newScope := &scope{
		policies: make(map[registry.ID]security.Policy, len(s.policies)-1),
	}
	for id, policy := range s.policies {
		if id != policyID {
			newScope.policies[id] = policy
		}
	}
	return newScope
}

func (s *scope) Evaluate(actor security.Actor, action, resource string, meta attrs.Bag) security.Result {
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

func (s *scope) Contains(policyID registry.ID) bool {
	_, exists := s.policies[policyID]
	return exists
}

func (s *scope) Policies() []security.Policy {
	policies := make([]security.Policy, 0, len(s.policies))
	for _, policy := range s.policies {
		policies = append(policies, policy)
	}
	return policies
}

var _ security.Scope = (*scope)(nil)
