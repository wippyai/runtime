package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
)

func TestWithSecurityConfig(t *testing.T) {
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)
	result := WithSecurityConfig(ctx, nil)
	assert.Equal(t, ctx, result)

	emptyConfig := &security.Config{}
	result = WithSecurityConfig(ctx, emptyConfig)
	_, ok := security.GetActor(result)
	assert.True(t, ok)

	actorConfig := &security.Config{
		Actor: security.Actor{ID: "test-user"},
	}
	result = WithSecurityConfig(ctx, actorConfig)
	_, ok = security.GetActor(result)
	assert.True(t, ok)

	policyConfig := &security.Config{
		Actor:        security.Actor{ID: "test-user"},
		Policies:     []registry.ID{{NS: "test", Name: "policy1"}},
		PolicyGroups: []registry.ID{{NS: "test", Name: "group1"}},
	}
	result = WithSecurityConfig(ctx, policyConfig)
	_, ok = security.GetActor(result)
	assert.True(t, ok)
	_, ok = security.GetScope(result)
	assert.False(t, ok)

	reg := NewPolicyRegistry(nil, nil)
	ctxWithReg := security.WithRegistry(ctx, reg)
	result = WithSecurityConfig(ctxWithReg, policyConfig)

	_, ok = security.GetActor(result)
	assert.True(t, ok)

	_, ok = security.GetScope(result)
	assert.False(t, ok)
}
