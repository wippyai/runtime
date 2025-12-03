package policy

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/service/security/policy"
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

func (p *Policy) Evaluate(actor security.Actor, action, resource string, meta attrs.Bag) security.Result {
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
	return matchesFilter(p.config.Policy.Actions, action)
}

func (p *Policy) matchesResource(resource string) bool {
	return matchesFilter(p.config.Policy.Resources, resource)
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
