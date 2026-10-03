// SPDX-License-Identifier: MPL-2.0

package process

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine"
	secsystem "github.com/wippyai/runtime/system/security"
)

type upgradePolicy struct{ result secapi.Result }

func (*upgradePolicy) ID() registry.ID { return registry.ParseID("test:upgrade") }

func (p *upgradePolicy) Evaluate(_ secapi.Actor, action, _ string, _ attrs.Bag) secapi.Result {
	if action == "process.upgrade" {
		return p.result
	}
	return secapi.Undefined
}

func TestUpgradeRefusedWhenExplicitlyDenied(t *testing.T) {
	l, _ := newLuaWithPID(t)
	require.NoError(t, secapi.SetActor(l.Context(), secapi.Actor{ID: "caller"}))
	require.NoError(t, secapi.SetScope(l.Context(), secsystem.NewScope([]secapi.Policy{&upgradePolicy{result: secapi.Deny}})))
	l.Push(lua.LString("app:worker"))

	// Denial happens before the upgrade request reaches the scheduler.
	require.Equal(t, 2, upgrade(l))
	require.Equal(t, lua.LNil, l.Get(-2))
	err, ok := l.Get(-1).(*lua.Error)
	require.True(t, ok)
	require.Equal(t, lua.PermissionDenied, err.Kind())
}

func TestUpgradeProceedsUnlessDenied(t *testing.T) {
	for _, result := range []secapi.Result{secapi.Undefined, secapi.Allow} {
		l, _ := newLuaWithPID(t)
		require.NoError(t, secapi.SetActor(l.Context(), secapi.Actor{ID: "caller"}))
		require.NoError(t, secapi.SetScope(l.Context(), secsystem.NewScope([]secapi.Policy{&upgradePolicy{result: result}})))
		l.Push(lua.LString("app:worker"))

		require.Equal(t, -1, upgrade(l))
		req, ok := l.Get(-1).(*engine.UpgradeRequest)
		require.True(t, ok)
		require.Equal(t, registry.ParseID("app:worker"), req.Source)
	}
}
