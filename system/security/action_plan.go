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

type resourcePrefix struct {
	prefix  string
	effects uint8
}
type actionPlan struct {
	exact    map[string]uint8
	prefixes []resourcePrefix
	dynamic  []security.Policy
	any      uint8
}

type actionPlans struct {
	byAction map[string]*actionPlan
	bytes    int
}

func cachedPlan(plans *actionPlans, action string) (*actionPlan, bool) {
	if plans == nil {
		return nil, false
	}
	p, known := plans.byAction[action]
	return p, known
}

func (p *actionPlan) match(resource string) uint8 {
	effects := p.any | p.exact[resource]
	for _, prefix := range p.prefixes {
		if strings.HasPrefix(resource, prefix.prefix) {
			effects |= prefix.effects
		}
	}
	return effects
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

// No actor, metadata, or resource from a caller enters a plan.
func (s *segment) plan(action string) *actionPlan {
	if s.opaque || len(action) > maxActionBytes {
		return nil
	}
	previous := s.plans.Load()
	p, known := cachedPlan(previous, action)
	if known {
		return p
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous = s.plans.Load()
	if p, known = cachedPlan(previous, action); known {
		return p
	}
	if previous != nil && (len(previous.byAction) >= maxActionPlans || previous.bytes >= maxSegmentPlanBytes) {
		return nil
	}
	named := false
	for _, entry := range s.ordered {
		if entry.descriptor.AppliesToAction(action) {
			named = true
			break
		}
	}
	if !named {
		return nil
	}
	p = &actionPlan{exact: make(map[string]uint8)}
	size := 256 + len(action)
	for _, entry := range s.ordered {
		d := entry.descriptor
		if !d.AppliesToAction(action) {
			continue
		}
		result, static := d.StaticEffect()
		if !static {
			p.dynamic = append(p.dynamic, entry.policy)
			size += 32
			if size > maxPlanBytes {
				break
			}
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
			if _, exists := p.exact[resource]; !exists {
				size += 96 + len(resource)
			}
			if size > maxPlanBytes {
				break
			}
			p.exact[resource] |= effect
		}
		if size <= maxPlanBytes {
			for _, prefix := range prefixes {
				size += 64 + len(prefix)
				if size > maxPlanBytes {
					break
				}
				p.prefixes = append(p.prefixes, resourcePrefix{prefix: prefix, effects: effect})
			}
		}
		if size > maxPlanBytes {
			break
		}
	}
	if size > maxPlanBytes {
		p = nil
		size = 256 + len(action)
	}
	next := &actionPlans{byAction: make(map[string]*actionPlan), bytes: size}
	if previous != nil {
		if previous.bytes+size > maxSegmentPlanBytes {
			return nil
		}
		next.bytes += previous.bytes
		for key, value := range previous.byAction {
			next.byAction[key] = value
		}
	}
	next.byAction[strings.Clone(action)] = p
	s.plans.Store(next)
	return p
}
func (s *segment) evaluate(actor security.Actor, action, resource string, meta attrs.Bag) security.Result {
	if !s.opaque && len(s.ordered) >= minPlannedPolicies {
		if p := s.plan(action); p != nil {
			return p.evaluate(actor, action, resource, meta)
		}
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
