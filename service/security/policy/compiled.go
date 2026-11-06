// SPDX-License-Identifier: MPL-2.0

package policy

import (
	"reflect"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/security"
)

type filter struct {
	exact    map[string]struct{}
	prefixes []string
	any      bool
}

func compileFilter(value any) filter {
	f := filter{exact: make(map[string]struct{})}
	add := func(pattern string) {
		pattern = strings.Clone(pattern)
		switch {
		case pattern == "*":
			f.any = true
		case len(pattern) > 2 && strings.HasSuffix(pattern, ".*"):
			f.prefixes = append(f.prefixes, pattern[:len(pattern)-2])
		case len(pattern) > 1 && strings.HasSuffix(pattern, "*"):
			f.prefixes = append(f.prefixes, pattern[:len(pattern)-1])
		default:
			f.exact[pattern] = struct{}{}
		}
	}
	switch v := value.(type) {
	case string:
		add(v)
	case []string:
		for _, s := range v {
			add(s)
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				add(s)
			}
		}
	}
	return f
}
func (f *filter) matches(value string) bool {
	if f.any {
		return true
	}
	if _, ok := f.exact[value]; ok {
		return true
	}
	for _, prefix := range f.prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
func (f *filter) describe() (bool, []string, []string) {
	exact := make([]string, 0, len(f.exact))
	for value := range f.exact {
		exact = append(exact, value)
	}
	return f.any, exact, slices.Clone(f.prefixes)
}

var policyIdentity atomic.Uint64

func nextIdentity() uint64 {
	// Exhaustion is terminal: identities must never wrap and alias an old policy.
	for {
		previous := policyIdentity.Load()
		if previous == ^uint64(0) {
			panic("security policy identity exhausted")
		}
		if policyIdentity.CompareAndSwap(previous, previous+1) {
			return previous + 1
		}
	}
}

// Condition values use the same scalar and collection shapes as decoded configs.
func cloneConditionValue(value any) any {
	return cloneConditionCollection(value, make(map[conditionValueKey]any))
}

type conditionValueKey struct {
	kind    reflect.Type
	address uintptr
	length  int
}

func cloneConditionCollection(value any, seen map[conditionValueKey]any) any {
	var key conditionValueKey
	switch value.(type) {
	case []any, map[string]any, attrs.Bag:
		v := reflect.ValueOf(value)
		if v.IsNil() {
			return value
		}
		key = conditionValueKey{kind: v.Type(), address: uintptr(v.UnsafePointer()), length: v.Len()}
		if copy, ok := seen[key]; ok {
			return copy
		}
	}
	switch v := value.(type) {
	case []any:
		result := make([]any, len(v))
		seen[key] = result
		for i, item := range v {
			result[i] = cloneConditionCollection(item, seen)
		}
		return result
	case []string:
		return slices.Clone(v)
	case []int:
		return slices.Clone(v)
	case map[string]any:
		result := make(map[string]any, len(v))
		seen[key] = result
		for k, item := range v {
			result[k] = cloneConditionCollection(item, seen)
		}
		return result
	case attrs.Bag:
		result := make(attrs.Bag, len(v))
		seen[key] = result
		for k, item := range v {
			result[k] = cloneConditionCollection(item, seen)
		}
		return result
	default:
		return value
	}
}

func (p *Policy) PolicyIdentity(candidate security.Policy) uint64 {
	own, ok := candidate.(*Policy)
	if !ok || own != p || p.self != p {
		return 0
	}
	return p.identity
}
func (p *Policy) AppliesToAction(action string) bool         { return p.actions.matches(action) }
func (p *Policy) ResourceFilter() (bool, []string, []string) { return p.resources.describe() }
func (p *Policy) StaticEffect() (security.Result, bool) {
	return p.getResult(), len(p.config.Policy.Conditions) == 0
}
func (p *ExprPolicy) PolicyIdentity(candidate security.Policy) uint64 {
	own, ok := candidate.(*ExprPolicy)
	if !ok || own != p || p.self != p {
		return 0
	}
	return p.identity
}
func (p *ExprPolicy) AppliesToAction(action string) bool         { return p.actions.matches(action) }
func (p *ExprPolicy) ResourceFilter() (bool, []string, []string) { return p.resources.describe() }
func (p *ExprPolicy) StaticEffect() (security.Result, bool)      { return security.Undefined, false }

var _ security.PolicyDescriptor = (*Policy)(nil)
var _ security.PolicyDescriptor = (*ExprPolicy)(nil)
