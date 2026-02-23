// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

type mockDirective struct{}

func (m *mockDirective) Expand(_ context.Context, _ registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
	return registry.DirectiveResult{}, nil
}

func TestWithKindDirective_RegistersDirective(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := NewMockRunner()
	hist := historymem.New()

	reg := NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		WithKindDirective("test.kind", &mockDirective{}),
	)
	require.NotNil(t, reg)
	assert.Len(t, reg.directivesByKind, 1)
	assert.Len(t, reg.directivesByKind["test.kind"], 1)
}

func TestWithKindDirective_NilDirectiveIgnored(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := NewMockRunner()
	hist := historymem.New()

	reg := NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		WithKindDirective("test.kind", nil),
	)
	require.NotNil(t, reg)
	assert.Nil(t, reg.directivesByKind)
}

func TestWithKindDirective_EmptyKindIgnored(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := NewMockRunner()
	hist := historymem.New()

	reg := NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		WithKindDirective("", &mockDirective{}),
	)
	require.NotNil(t, reg)
	assert.Nil(t, reg.directivesByKind)
}

func TestWithKindDirective_MultipleDirectivesSameKind(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := NewMockRunner()
	hist := historymem.New()

	reg := NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		WithKindDirective("test.kind", &mockDirective{}),
		WithKindDirective("test.kind", &mockDirective{}),
	)
	require.NotNil(t, reg)
	assert.Len(t, reg.directivesByKind["test.kind"], 2)
}

func TestWithKindDirective_MultipleKinds(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := NewMockRunner()
	hist := historymem.New()

	reg := NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		WithKindDirective("kind.a", &mockDirective{}),
		WithKindDirective("kind.b", &mockDirective{}),
	)
	require.NotNil(t, reg)
	assert.Len(t, reg.directivesByKind, 2)
}
