// SPDX-License-Identifier: MPL-2.0

package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/system/eventbus"
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
		Policies:     []registry.ID{registry.NewID("test", "policy1")},
		PolicyGroups: []registry.ID{registry.NewID("test", "group1")},
	}
	result = WithSecurityConfig(ctx, policyConfig)
	_, ok = security.GetActor(result)
	assert.True(t, ok)
	_, ok = security.GetScope(result)
	assert.False(t, ok)

	reg := NewPolicyRegistry(eventbus.NewBus(), nil)
	ctxWithReg := security.WithRegistry(ctx, reg)
	result = WithSecurityConfig(ctxWithReg, policyConfig)

	_, ok = security.GetActor(result)
	assert.True(t, ok)
	_, ok = security.GetScope(result)
	assert.False(t, ok)
}
