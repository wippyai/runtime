// SPDX-License-Identifier: MPL-2.0

package security

import (
	"sync/atomic"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/security"
)

const (
	allowBit uint8 = 1 << iota
	denyBit
)

type resourceRadix struct{ radix[uint8] }

func (r *resourceRadix) insert(prefix string, effects uint8) {
	r.node(prefix).value |= effects
}
func (r *resourceRadix) match(resource string) uint8 {
	var effects uint8
	r.walk(resource, func(value uint8) { effects |= value })
	return effects
}

type actionPlan struct {
	prefixes resourceRadix
	exact    map[string]uint8
	dynamic  []security.Policy
	any      uint8
}

type planSlot struct {
	value atomic.Pointer[actionPlan]
}

type patternClass struct {
	slot       planSlot
	parent     *patternClass
	candidates []int
}

type actionPlans struct {
	byAction map[string]*planSlot
	patterns actionRadix
	opaque   []security.Policy
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

// The index owns only policy-supplied exact names and literal prefixes. Every
// deepest matching prefix identifies one pattern set, independent of caller keys.
func (s *segment) actionIndex() *actionPlans {
	if index := s.plans.Load(); index != nil {
		return index
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if index := s.plans.Load(); index != nil {
		return index
	}
	index := &actionPlans{byAction: make(map[string]*planSlot)}
	for i, entry := range s.ordered {
		if entry.descriptor == nil {
			index.opaque = append(index.opaque, entry.policy)
			continue
		}
		anyAction, exact, prefixes := entry.descriptor.ActionFilter()
		for _, name := range exact {
			if index.byAction[name] == nil {
				index.byAction[name] = new(planSlot)
			}
		}
		if anyAction {
			index.patterns.add("", i)
		}
		for _, prefix := range prefixes {
			index.patterns.add(prefix, i)
		}
	}
	if len(index.opaque) != 0 && index.patterns.value == nil {
		index.patterns.value = new(patternClass)
	}
	index.patterns.link(nil)
	s.plans.Store(index)
	return index
}

func (s *segment) plan(action string) *actionPlan {
	index := s.actionIndex()
	slot := index.byAction[action]
	var class *patternClass
	if slot == nil {
		class = index.patterns.match(action)
		if class == nil {
			return nil
		}
		slot = &class.slot
	}
	if p := slot.value.Load(); p != nil {
		return p
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := slot.value.Load(); p != nil {
		return p
	}
	p := &actionPlan{exact: make(map[string]uint8), dynamic: append([]security.Policy(nil), index.opaque...)}
	if class == nil {
		for _, entry := range s.ordered {
			if entry.descriptor != nil && entry.descriptor.AppliesToAction(action) {
				p.add(entry)
			}
		}
	} else {
		seen := make([]bool, len(s.ordered))
		for current := class; current != nil; current = current.parent {
			for _, i := range current.candidates {
				if !seen[i] {
					seen[i] = true
					p.add(s.ordered[i])
				}
			}
		}
	}
	slot.value.Store(p)
	return p
}

func (p *actionPlan) add(entry policyEntry) {
	d := entry.descriptor
	result, static := d.StaticEffect()
	if !static {
		p.dynamic = append(p.dynamic, entry.policy)
		return
	}
	var effect uint8
	switch result {
	case security.Allow:
		effect = allowBit
	case security.Deny:
		effect = denyBit
	default:
		return
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

func (s *segment) evaluate(actor security.Actor, action, resource string, meta attrs.Bag) security.Result {
	if p := s.plan(action); p != nil {
		return p.evaluate(actor, action, resource, meta)
	}
	return security.Undefined
}
