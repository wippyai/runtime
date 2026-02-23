// SPDX-License-Identifier: MPL-2.0

package runner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/registry"
)

func TestKindDispatchPolicy_NilPolicy(t *testing.T) {
	var p *KindDispatchPolicy
	op := registry.Operation{
		Entry: registry.Entry{Kind: "service"},
	}
	assert.Equal(t, registry.DispatchEvents, p.Mode(op))
}

func TestKindDispatchPolicy_EmptyInternalKinds(t *testing.T) {
	p := NewKindDispatchPolicy(nil)
	op := registry.Operation{
		Entry: registry.Entry{Kind: "service"},
	}
	assert.Equal(t, registry.DispatchEvents, p.Mode(op))
}

func TestKindDispatchPolicy_InternalKind(t *testing.T) {
	p := NewKindDispatchPolicy([]registry.Kind{"ns.dependency", "ns.internal"})
	op := registry.Operation{
		Entry: registry.Entry{Kind: "ns.dependency"},
	}
	assert.Equal(t, registry.DispatchInternal, p.Mode(op))
}

func TestKindDispatchPolicy_ExternalKind(t *testing.T) {
	p := NewKindDispatchPolicy([]registry.Kind{"ns.dependency"})
	op := registry.Operation{
		Entry: registry.Entry{Kind: "service"},
	}
	assert.Equal(t, registry.DispatchEvents, p.Mode(op))
}

func TestKindDispatchPolicy_SkipsEmptyKinds(t *testing.T) {
	p := NewKindDispatchPolicy([]registry.Kind{"", "ns.internal", ""})
	assert.Len(t, p.internal, 1)
	assert.Contains(t, p.internal, registry.Kind("ns.internal"))
}

func TestKindDispatchPolicy_MultipleInternalKinds(t *testing.T) {
	kinds := []registry.Kind{"kind.a", "kind.b", "kind.c"}
	p := NewKindDispatchPolicy(kinds)

	for _, k := range kinds {
		op := registry.Operation{Entry: registry.Entry{Kind: k}}
		assert.Equal(t, registry.DispatchInternal, p.Mode(op), "expected internal for kind %s", k)
	}

	op := registry.Operation{Entry: registry.Entry{Kind: "kind.d"}}
	assert.Equal(t, registry.DispatchEvents, p.Mode(op))
}
