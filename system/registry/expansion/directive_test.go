package expansion

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

// --- DependencyDirective ---

func TestDependencyDirective_Expand(t *testing.T) {
	called := false
	d := NewDependencyDirective(func(ctx context.Context, op registry.Operation, snapshot registry.State) (registry.DirectiveResult, error) {
		called = true
		return registry.DirectiveResult{Applied: true}, nil
	})

	result, err := d.Expand(context.Background(), registry.Operation{}, nil)
	require.NoError(t, err)
	assert.True(t, result.Applied)
	assert.True(t, called)
}

func TestDependencyDirective_Expand_NilDirective(t *testing.T) {
	var d *DependencyDirective
	result, err := d.Expand(context.Background(), registry.Operation{}, nil)
	assert.NoError(t, err)
	assert.False(t, result.Applied)
}

func TestDependencyDirective_Expand_NilFunc(t *testing.T) {
	d := &DependencyDirective{}
	result, err := d.Expand(context.Background(), registry.Operation{}, nil)
	assert.NoError(t, err)
	assert.False(t, result.Applied)
}

func TestDependencyDirective_Expand_CancelledContext(t *testing.T) {
	d := NewDependencyDirective(func(ctx context.Context, op registry.Operation, snapshot registry.State) (registry.DirectiveResult, error) {
		t.Fatal("should not be called")
		return registry.DirectiveResult{}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := d.Expand(ctx, registry.Operation{}, nil)
	assert.ErrorIs(t, err, context.Canceled)
}

// --- Error constructors ---

func TestNewDirectiveResultInvalidError(t *testing.T) {
	id := registry.NewID("app", "test")
	kind := registry.Kind("function.lua")

	err := NewDirectiveResultInvalidError(id, kind)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "directive returned data without Applied=true")
}

func TestNewDirectiveExpansionConflictError(t *testing.T) {
	id := registry.NewID("app", "test")

	err := NewDirectiveExpansionConflictError(id)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "expansion operation conflicts")
}
