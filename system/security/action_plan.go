// SPDX-License-Identifier: MPL-2.0

package security

import (
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/security"
)

const (
	allowBit uint8 = 1 << iota
	denyBit
)

// A compressed byte radix tree joins effects along the resource's path. Neither
// sibling count nor the number of stored prefixes affects the number of steps.
type resourceRadix struct {
	children map[byte]*resourceRadix
	prefix   string
	effects  uint8
}

func (r *resourceRadix) insert(prefix string, effects uint8) {
	for len(prefix) > 0 {
		if r.children == nil {
			r.children = make(map[byte]*resourceRadix)
		}
		child := r.children[prefix[0]]
		if child == nil {
			r.children[prefix[0]] = &resourceRadix{prefix: prefix, effects: effects}
			return
		}
		common := 0
		for common < len(prefix) && common < len(child.prefix) && prefix[common] == child.prefix[common] {
			common++
		}
		if common < len(child.prefix) {
			suffix := &resourceRadix{prefix: child.prefix[common:], effects: child.effects, children: child.children}
			child.prefix = child.prefix[:common]
			child.effects = 0
			child.children = map[byte]*resourceRadix{suffix.prefix[0]: suffix}
		}
		prefix = prefix[common:]
		r = child
	}
	r.effects |= effects
}
func (r *resourceRadix) match(resource string) uint8 {
	effects := r.effects
	for len(resource) > 0 {
		child := r.children[resource[0]]
		if child == nil || !strings.HasPrefix(resource, child.prefix) {
			break
		}
		effects |= child.effects
		resource = resource[len(child.prefix):]
		r = child
	}
	return effects
}

type actionPlan struct {
	exact    map[string]uint8
	dynamic  []security.Policy
	prefixes resourceRadix
	any      uint8
}

type actionPlans struct {
	byAction     map[string]*actionPlan
	exactActions map[string]string
	patterns     int
}

func cachedPlan(plans *actionPlans, action string) (*actionPlan, bool) {
	if plans == nil {
		return nil, false
	}
	p, known := plans.byAction[action]
	return p, known
}
func (p *actionPlan) match(resource string) uint8 {
	return p.any | p.exact[resource] | p.prefixes.match(resource)
}
func (p *actionPlan) evaluate(actor security.Actor, action, resource string, meta attrs.Bag) security.Result {
	effects := p.match(resource)
	if effects&denyBit != 0 {
		return security.Deny
	}
	for _, candidate := range p.dynamic {
		switch candidate.Evaluate(actor, action, resource, meta) {
		case security.Deny:
			return security.Deny
		case security.Allow:
			effects |= allowBit
		}
	}
	if effects&allowBit != 0 {
		return security.Allow
	}
	return security.Undefined
}

// Exact action keys and all resource data belong to policies. Only a bounded
// number of short pattern-minted keys are retained; no caller data enters a plan.
func (s *segment) plan(action string) *actionPlan {
	previous := s.plans.Load()
	if p, known := cachedPlan(previous, action); known {
		return p
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous = s.plans.Load()
	if p, known := cachedPlan(previous, action); known {
		return p
	}
	p := s.buildPlan(action)
	if p == nil {
		return nil
	}
	exactActions := map[string]string(nil)
	if previous != nil {
		exactActions = previous.exactActions
	} else {
		exactActions = make(map[string]string)
		for _, entry := range s.ordered {
			if entry.descriptor != nil {
				_, exact, _ := entry.descriptor.ActionFilter()
				for _, name := range exact {
					if _, exists := exactActions[name]; !exists {
						exactActions[name] = name
					}
				}
			}
		}
	}
	key, exact := exactActions[action]
	if !exact {
		if len(action) > maxActionBytes || previous != nil && previous.patterns >= maxActionPlans {
			return p
		}
		key = strings.Clone(action)
	}
	next := &actionPlans{byAction: make(map[string]*actionPlan), exactActions: exactActions}
	if previous != nil {
		next.patterns = previous.patterns
		for k, v := range previous.byAction {
			next.byAction[k] = v
		}
	}
	if !exact {
		next.patterns++
	}
	next.byAction[key] = p
	s.plans.Store(next)
	return p
}

func (s *segment) buildPlan(action string) *actionPlan {
	named := false
	for _, entry := range s.ordered {
		if entry.descriptor != nil && entry.descriptor.AppliesToAction(action) {
			named = true
			break
		}
	}
	if !named {
		return nil
	}
	p := &actionPlan{exact: make(map[string]uint8)}
	for _, entry := range s.ordered {
		d := entry.descriptor
		if d == nil {
			p.dynamic = append(p.dynamic, entry.policy)
			continue
		}
		if !d.AppliesToAction(action) {
			continue
		}
		result, static := d.StaticEffect()
		if !static {
			p.dynamic = append(p.dynamic, entry.policy)
			continue
		}
		var effect uint8
		switch result {
		case security.Allow:
			effect = allowBit
		case security.Deny:
			effect = denyBit
		default:
			continue
		}
		anyResource, exact, prefixes := d.ResourceFilter()
		if anyResource {
			p.any |= effect
		}
		for _, resource := range exact {
			p.exact[resource] |= effect
		}
		for _, prefix := range prefixes {
			p.prefixes.insert(prefix, effect)
		}
	}
	return p
}
func (s *segment) evaluate(actor security.Actor, action, resource string, meta attrs.Bag) security.Result {
	if p := s.plan(action); p != nil {
		return p.evaluate(actor, action, resource, meta)
	}
	return s.evaluatePolicies(actor, action, resource, meta)
}
func (s *segment) evaluatePolicies(actor security.Actor, action, resource string, meta attrs.Bag) security.Result {
	result := security.Undefined
	for _, entry := range s.ordered {
		if entry.descriptor != nil && !entry.descriptor.AppliesToAction(action) {
			continue
		}
		r := entry.policy.Evaluate(actor, action, resource, meta)
		if r == security.Deny {
			return r
		}
		if r != security.Undefined {
			result = r
		}
	}
	return result
}
