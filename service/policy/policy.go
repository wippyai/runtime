package policy

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/api/service/policy"
)

// Policy implements the security.Policy interface
type Policy struct {
	// id is the unique identifier of the policy
	id registry.ID

	// config contains the policy definition and configuration
	config *policy.Config

	// evaluator is used to evaluate conditions
	evaluator *ConditionEvaluator
}

// NewPolicy creates a new policy from a configuration
func NewPolicy(id registry.ID, config *policy.Config) (*Policy, error) {
	// Validate the configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &Policy{
		id:        id,
		config:    config,
		evaluator: NewConditionEvaluator(),
	}, nil
}

// ID returns the policy's unique identifier
func (p *Policy) ID() registry.ID {
	return p.id
}

// Evaluate determines if the action on resource is allowed/denied based on the policy
func (p *Policy) Evaluate(actor security.Actor, action, resource string, meta registry.Metadata) security.Result {
	// Check if the action matches
	if !p.matchesAction(action) {
		return security.Undefined
	}

	// Check if the resource matches
	if !p.matchesResource(resource) {
		return security.Undefined
	}

	// If there are no conditions, the policy applies unconditionally
	if len(p.config.Policy.Conditions) == 0 {
		return p.getResult()
	}

	// Evaluate all conditions, all must be true for the policy to apply
	for _, condition := range p.config.Policy.Conditions {
		match, err := p.evaluator.EvaluateCondition(condition, actor, action, resource, meta)
		if err != nil || !match {
			return security.Undefined
		}
	}

	// All conditions matched, return the policy's effect
	return p.getResult()
}

// matchesAction checks if the given action matches the policy's actions
func (p *Policy) matchesAction(action string) bool {
	switch actions := p.config.Policy.Actions.(type) {
	case string:
		// Global wildcard matches all actions
		if actions == "*" {
			return true
		}
		// Check for pattern matching with wildcard suffix
		return matchesPattern(actions, action)
	case []any:
		for _, a := range actions {
			if str, ok := a.(string); ok {
				// Global wildcard matches all actions
				if str == "*" {
					return true
				}
				// Check for pattern matching with wildcard suffix
				if matchesPattern(str, action) {
					return true
				}
			}
		}
	}
	return false
}

// matchesResource checks if the given resource matches the policy's resources
func (p *Policy) matchesResource(resource string) bool {
	switch resources := p.config.Policy.Resources.(type) {
	case string:
		// Global wildcard matches all resources
		if resources == "*" {
			return true
		}
		// Check for pattern matching with wildcard suffix
		return matchesPattern(resources, resource)
	case []any:
		for _, r := range resources {
			if str, ok := r.(string); ok {
				// Global wildcard matches all resources
				if str == "*" {
					return true
				}
				// Check for pattern matching with wildcard suffix
				if matchesPattern(str, resource) {
					return true
				}
			}
		}
	}
	return false
}

// matchesPattern checks if the value matches the pattern, supporting wildcard suffixes
// For example:
// - "registry.read.*" matches "registry.read.document" or "registry.read.anything"
// - "functions:call.*" matches "functions:call.lambda" or "functions:call.something"
// - "exact.match" only matches "exact.match"
func matchesPattern(pattern, value string) bool {
	// Global wildcard matches everything
	if pattern == "*" {
		return true
	}

	// If it's an exact match, return true immediately
	if pattern == value {
		return true
	}

	// Check if pattern ends with ".*" wildcard
	if len(pattern) > 2 && pattern[len(pattern)-2:] == ".*" {
		// Get the prefix (everything before the ".*")
		prefix := pattern[:len(pattern)-2]
		// Check if value starts with the prefix
		return len(value) >= len(prefix) && value[:len(prefix)] == prefix
	}

	// Check if pattern ends with "*" wildcard (without dot)
	if len(pattern) > 1 && pattern[len(pattern)-1:] == "*" {
		// Get the prefix (everything before the "*")
		prefix := pattern[:len(pattern)-1]
		// Check if value starts with the prefix
		return len(value) >= len(prefix) && value[:len(prefix)] == prefix
	}

	return false
}

// getResult converts the policy's effect to a security.Result
func (p *Policy) getResult() security.Result {
	switch p.config.Policy.Effect {
	case policy.Allow:
		return security.Allow
	case policy.Deny:
		return security.Deny
	default:
		return security.Undefined
	}
}
