// SPDX-License-Identifier: MPL-2.0

package runner

import "github.com/wippyai/runtime/api/registry"

// KindDispatchPolicy dispatches operations based on entry kinds.
type KindDispatchPolicy struct {
	internal map[registry.Kind]struct{}
}

// NewKindDispatchPolicy creates a policy that treats the given kinds as internal.
func NewKindDispatchPolicy(internalKinds []registry.Kind) *KindDispatchPolicy {
	p := &KindDispatchPolicy{internal: make(map[registry.Kind]struct{}, len(internalKinds))}
	for _, kind := range internalKinds {
		if kind == "" {
			continue
		}
		p.internal[kind] = struct{}{}
	}
	return p
}

// Mode implements registry.DispatchPolicy.
func (p *KindDispatchPolicy) Mode(op registry.Operation) registry.DispatchMode {
	if p == nil || len(p.internal) == 0 {
		return registry.DispatchEvents
	}
	if _, ok := p.internal[op.Entry.Kind]; ok {
		return registry.DispatchInternal
	}
	return registry.DispatchEvents
}
