// SPDX-License-Identifier: MPL-2.0

package policy

import (
	"slices"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/service/security/policy"
)

type Policy struct {
	config             *policy.Config
	evaluator          *ConditionEvaluator
	self               *Policy
	id                 registry.ID
	actions, resources filter
	identity           uint64
}

func NewPolicy(id registry.ID, config *policy.Config) (*Policy, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	owned := *config
	owned.Policy.Conditions = slices.Clone(config.Policy.Conditions)
	for i := range owned.Policy.Conditions {
		owned.Policy.Conditions[i].Value = cloneConditionValue(owned.Policy.Conditions[i].Value)
	}
	owned.Policy.Actions = nil
	owned.Policy.Resources = nil
	owned.Groups = nil
	evaluator, err := NewConditionEvaluator(owned.Policy.Conditions)
	if err != nil {
		return nil, err
	}

	p := &Policy{
		id:        id,
		config:    &owned,
		actions:   compileFilter(config.Policy.Actions),
		resources: compileFilter(config.Policy.Resources),
		identity:  nextIdentity(),
		evaluator: evaluator,
	}
	p.self = p
	return p, nil
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
	return p.actions.matches(action)
}

func (p *Policy) matchesResource(resource string) bool {
	return p.resources.matches(resource)
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

var _ security.Policy = (*Policy)(nil)
