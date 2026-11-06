// SPDX-License-Identifier: MPL-2.0

package security

import "strings"

// radix visits the values on a raw byte prefix path in time proportional to the
// input length. Labels reference policy-owned strings, never queried strings.
type radix[T any] struct {
	value    T
	children map[byte]*radix[T]
	prefix   string
}

func (r *radix[T]) node(prefix string) *radix[T] {
	for len(prefix) > 0 {
		if r.children == nil {
			r.children = make(map[byte]*radix[T])
		}
		child := r.children[prefix[0]]
		if child == nil {
			child = &radix[T]{prefix: prefix}
			r.children[prefix[0]] = child
			return child
		}
		common := 0
		for common < len(prefix) && common < len(child.prefix) && prefix[common] == child.prefix[common] {
			common++
		}
		if common < len(child.prefix) {
			suffix := &radix[T]{prefix: child.prefix[common:], value: child.value, children: child.children}
			child.prefix = child.prefix[:common]
			var zero T
			child.value = zero
			child.children = map[byte]*radix[T]{suffix.prefix[0]: suffix}
		}
		prefix = prefix[common:]
		r = child
	}
	return r
}

func (r *radix[T]) walk(key string, visit func(T)) {
	visit(r.value)
	for len(key) > 0 {
		child := r.children[key[0]]
		if child == nil || !strings.HasPrefix(key, child.prefix) {
			return
		}
		visit(child.value)
		key = key[len(child.prefix):]
		r = child
	}
}

type actionRadix struct{ radix[*patternClass] }

func (r *actionRadix) add(prefix string, candidate int) {
	node := r.node(prefix)
	if node.value == nil {
		node.value = new(patternClass)
	}
	node.value.candidates = append(node.value.candidates, candidate)
}
func (r *actionRadix) match(action string) *patternClass {
	var result *patternClass
	r.walk(action, func(value *patternClass) {
		if value != nil {
			result = value
		}
	})
	return result
}
func (r *actionRadix) link(parent *patternClass) { linkPatterns(&r.radix, parent) }
func linkPatterns(r *radix[*patternClass], parent *patternClass) {
	if r.value != nil {
		r.value.parent = parent
		parent = r.value
	}
	for _, child := range r.children {
		linkPatterns(child, parent)
	}
}
