// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	secsystem "github.com/wippyai/runtime/system/security"
)

type spawnerTestPolicy struct{ contextAllowed, securityAllowed bool }

func (*spawnerTestPolicy) ID() registry.ID { return registry.NewID("test", "spawner") }
func (p *spawnerTestPolicy) Evaluate(_ secapi.Actor, action, _ string, _ attrs.Bag) secapi.Result {
	if action == "process.context" && p.contextAllowed || action == "process.security" && p.securityAllowed {
		return secapi.Allow
	}
	return secapi.Deny
}

func spawnerSecurityLua(t *testing.T, policy *spawnerTestPolicy) (*lua.LState, secapi.Scope) {
	t.Helper()
	l, _ := newLuaWithPID(t)
	scope := secsystem.NewScope([]secapi.Policy{policy})
	require.NoError(t, secapi.SetActor(l.Context(), secapi.Actor{ID: "caller"}))
	require.NoError(t, secapi.SetScope(l.Context(), scope))
	actorUD, scopeUD := l.NewUserData(), l.NewUserData()
	actorUD.Value = secapi.Actor{ID: "replacement"}
	scopeUD.Value = secsystem.NewScope(nil)
	l.SetGlobal("other_actor", actorUD)
	l.SetGlobal("other_scope", scopeUD)
	return l, scope
}

func TestSpawnerInheritedSecurityAllowsContextComposition(t *testing.T) {
	for _, chain := range []string{
		`process.with_options({marker = "option"}):with_context({greeting = "hello"})`,
		`process.with_context({greeting = "hello"}):with_options({marker = "option"})`,
		`process.with_context({greeting = "before"}):with_context({greeting = "hello"}):with_options({marker = "option"})`,
	} {
		t.Run(chain, func(t *testing.T) {
			l, scope := spawnerSecurityLua(t, &spawnerTestPolicy{contextAllowed: true})
			require.NoError(t, l.DoString("result = "+chain))
			spawner := l.GetGlobal("result").(*lua.LUserData).Value.(*Spawner)
			child, frame := ctxapi.OpenFrameContext(context.Background())
			defer ctxapi.ReleaseFrameContext(frame)
			require.NoError(t, frame.SetMultiple(buildSpawnerContext(spawner)...))
			actor, ok := secapi.GetActor(child)
			require.True(t, ok)
			require.Equal(t, "caller", actor.ID)
			childScope, ok := secapi.GetScope(child)
			require.True(t, ok)
			require.Same(t, scope, childScope)
			greeting, ok := ctxapi.GetValues(child).Get("greeting")
			require.True(t, ok)
			require.Equal(t, "hello", greeting)
			require.Equal(t, "option", spawner.options.GetString("marker", ""))
		})
	}
}

func TestSpawnerExplicitSecurityStillRequiresPermission(t *testing.T) {
	for _, override := range []string{`:with_actor(other_actor)`, `:with_scope(other_scope)`} {
		t.Run(override, func(t *testing.T) {
			policy := &spawnerTestPolicy{contextAllowed: true}
			l, _ := spawnerSecurityLua(t, policy)
			require.ErrorContains(t, l.DoString("result = process.with_options({})"+override), "custom security context")
			policy.securityAllowed = true
			require.NoError(t, l.DoString("result = process.with_options({})"+override+`:with_options({}):with_name("child"):with_context({first = true})`))
			// A second context edit must retain the existing reauthorization check
			// after all cloning paths, including the first with_context call.
			policy.securityAllowed = false
			require.ErrorContains(t, l.DoString(`result:with_context({second = true})`), "custom security context")
		})
	}
}

func TestSpawnerContextPermissionStillRequired(t *testing.T) {
	l, _ := spawnerSecurityLua(t, &spawnerTestPolicy{})
	require.ErrorContains(t, l.DoString(`process.with_options({})`), "custom options")
	require.ErrorContains(t, l.DoString(`process.with_context({})`), "custom context")
}
