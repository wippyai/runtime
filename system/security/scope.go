// SPDX-License-Identifier: MPL-2.0

package security

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
)

const (
	maxScopeSegments = 8
	// Reserve one slot for compacted declarations and single-policy additions.
	maxLargeSegments     = maxScopeSegments - 1
	largeSegmentPolicies = 64
)

type scope struct{ segments []*segment }

// NewScope creates an immutable collection. Duplicate IDs use the last policy.
func NewScope(policies []security.Policy) security.Scope { return makeScope(policies) }

func (s *scope) empty() bool {
	for _, part := range s.segments {
		if len(part.ordered) != 0 {
			return false
		}
	}
	return true
}

func (s *scope) entry(id registry.ID) (policyEntry, bool) {
	for _, segment := range s.segments {
		if i, ok := segment.byID[id]; ok {
			return segment.ordered[i], true
		}
	}
	return policyEntry{}, false
}
func (s *scope) With(policy security.Policy) security.Scope {
	entry := describePolicy(policy)
	if existing, ok := s.entry(entry.id); ok {
		if entry.identity != 0 && entry.identity == existing.identity {
			return s
		}
		return NewScope(append(s.Policies(), policy))
	}
	return s.appendSegments(sharedSegments.fromEntry(entry).single.segments)
}

// merge checks only the declaration against each bounded segment index. Existing
// policies are not enumerated unless an ID replaces a different policy object.
func (s *scope) merge(added *scope) *scope {
	var pending [maxScopeSegments]*segment
	segments := pending[:0]
	for _, part := range added.segments {
		duplicates := 0
		for _, entry := range part.ordered {
			if existing, ok := s.entry(entry.id); ok {
				if entry.identity == 0 || entry.identity != existing.identity {
					return makeScope(append(s.Policies(), added.Policies()...))
				}
				duplicates++
			}
		}
		if duplicates == len(part.ordered) {
			continue
		}
		if duplicates == 0 {
			segments = append(segments, part)
			continue
		}
		list := make([]security.Policy, 0, len(part.ordered)-duplicates)
		for _, entry := range part.ordered {
			if !s.Contains(entry.id) {
				list = append(list, entry.policy)
			}
		}
		segments = append(segments, makeScope(list).segments...)
	}
	return s.appendSegments(segments)
}
func (s *scope) appendSegments(added []*segment) *scope {
	if len(added) == 0 {
		return s
	}
	if len(s.segments) == 1 && len(s.segments[0].ordered) == 0 && len(added) == 1 {
		return &added[0].single
	}
	parts := make([]*segment, 0, len(s.segments)+len(added))
	parts = append(parts, s.segments...)
	parts = append(parts, added...)
	result := &scope{segments: parts}
	if len(parts) > maxScopeSegments {
		return result.compact()
	}
	return result
}

// compact preserves large shared bases and combines only small additions. The
// reserved tail slot keeps both index lookups and repeated With chains bounded.
func (s *scope) compact() *scope {
	large, smallPolicies := 0, 0
	for _, part := range s.segments {
		if len(part.ordered) >= largeSegmentPolicies {
			large++
		} else {
			smallPolicies += len(part.ordered)
		}
	}
	if large > maxLargeSegments {
		return makeScope(s.Policies())
	}
	parts := make([]*segment, 0, maxScopeSegments)
	policies := make([]security.Policy, 0, smallPolicies)
	for _, part := range s.segments {
		if len(part.ordered) >= largeSegmentPolicies {
			parts = append(parts, part)
		} else {
			for _, entry := range part.ordered {
				policies = append(policies, entry.policy)
			}
		}
	}
	if len(policies) > 0 {
		parts = append(parts, sharedSegments.fromList(policies))
	}
	return &scope{segments: parts}
}

func (s *scope) Without(policyID registry.ID) security.Scope {
	for i, part := range s.segments {
		if _, ok := part.byID[policyID]; !ok {
			continue
		}
		list := make([]security.Policy, 0, len(part.ordered)-1)
		for _, entry := range part.ordered {
			if entry.id != policyID {
				list = append(list, entry.policy)
			}
		}
		if len(s.segments) == 1 {
			return NewScope(list)
		}
		parts := append([]*segment(nil), s.segments[:i]...)
		if len(list) > 0 {
			parts = append(parts, makeScope(list).segments...)
		}
		parts = append(parts, s.segments[i+1:]...)
		return &scope{segments: parts}
	}
	return s
}
func (s *scope) Evaluate(actor security.Actor, action, resource string, meta attrs.Bag) security.Result {
	result := security.Undefined
	for _, part := range s.segments {
		switch part.evaluate(actor, action, resource, meta) {
		case security.Deny:
			return security.Deny
		case security.Allow:
			result = security.Allow
		}
	}
	return result
}
func (s *scope) Contains(policyID registry.ID) bool { _, exists := s.entry(policyID); return exists }
func (s *scope) Policies() []security.Policy {
	n := 0
	for _, part := range s.segments {
		n += len(part.ordered)
	}
	policies := make([]security.Policy, 0, n)
	for _, part := range s.segments {
		for _, entry := range part.ordered {
			policies = append(policies, entry.policy)
		}
	}
	return policies
}

var _ security.Scope = (*scope)(nil)
