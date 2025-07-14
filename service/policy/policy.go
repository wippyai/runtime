package policy

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/api/service/policy"
)

type Policy struct {
	id        registry.ID
	config    *policy.Config
	evaluator *ConditionEvaluator
}

func NewPolicy(id registry.ID, config *policy.Config) (*Policy, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	evaluator, err := NewConditionEvaluator(config.Policy.Conditions)
	if err != nil {
		return nil, err
	}

	return &Policy{
		id:        id,
		config:    config,
		evaluator: evaluator,
	}, nil
}

func (p *Policy) ID() registry.ID {
	return p.id
}

func (p *Policy) Evaluate(actor security.Actor, action, resource string, meta registry.Metadata) security.Result {
	if !p.matchesAction(action) {
		return security.Undefined
	}

	if !p.matchesResource(resource) {
		return security.Undefined
	}

	if len(p.config.Policy.Conditions) == 0 {
		return p.getResult()
	}

	for _, condition := range p.config.Policy.Conditions {
		match, err := p.evaluator.EvaluateCondition(condition, actor, action, resource, meta)
		if err != nil || !match {
			return security.Undefined
		}
	}

	return p.getResult()
}

func (p *Policy) matchesAction(action string) bool {
	switch actions := p.config.Policy.Actions.(type) {
	case string:
		if actions == "*" {
			return true
		}
		return matchesPattern(actions, action)
	case []any:
		for _, a := range actions {
			if str, ok := a.(string); ok {
				if str == "*" {
					return true
				}
				if matchesPattern(str, action) {
					return true
				}
			}
		}
	}
	return false
}

func (p *Policy) matchesResource(resource string) bool {
	switch resources := p.config.Policy.Resources.(type) {
	case string:
		if resources == "*" {
			return true
		}
		return matchesPattern(resources, resource)
	case []any:
		for _, r := range resources {
			if str, ok := r.(string); ok {
				if str == "*" {
					return true
				}
				if matchesPattern(str, resource) {
					return true
				}
			}
		}
	}
	return false
}

func matchesPattern(pattern, value string) bool {
	if pattern == "*" {
		return true
	}

	if pattern == value {
		return true
	}

	if len(pattern) > 2 && pattern[len(pattern)-2:] == ".*" {
		prefix := pattern[:len(pattern)-2]
		return len(value) >= len(prefix) && value[:len(prefix)] == prefix
	}

	if len(pattern) > 1 && pattern[len(pattern)-1:] == "*" {
		prefix := pattern[:len(pattern)-1]
		return len(value) >= len(prefix) && value[:len(prefix)] == prefix
	}

	return false
}

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
