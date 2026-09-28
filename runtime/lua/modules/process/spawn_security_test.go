// SPDX-License-Identifier: MPL-2.0

package process

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	secsystem "github.com/wippyai/runtime/system/security"
)

// Allow the entry and relationship permissions while independently controlling
// the destination host. This catches spawn variants that skip the second axis.
type spawnHostPolicy struct{ allowHost bool }

func (*spawnHostPolicy) ID() registry.ID { return registry.ParseID("test:spawn-host") }

func (p *spawnHostPolicy) Evaluate(_ secapi.Actor, action, resource string, _ attrs.Bag) secapi.Result {
	switch action {
	case "process.spawn", "process.spawn.monitored", "process.spawn.linked":
		if resource == "app:worker" {
			return secapi.Allow
		}
	case "process.host":
		if p.allowHost && resource == "app:processes" {
			return secapi.Allow
		}
	}
	return secapi.Deny
}

func TestSpawnVariantsRequireHostPermission(t *testing.T) {
	variants := []struct {
		call func(*lua.LState) int
		name string
	}{
		{name: "spawn", call: spawn},
		{name: "spawn_monitored", call: spawnMonitored},
		{name: "spawn_linked", call: spawnLinked},
		{name: "spawn_linked_monitored", call: spawnLinkedMonitored},
	}
	for _, variant := range variants {
		for _, allowed := range []bool{false, true} {
			label := "denied"
			if allowed {
				label = "allowed"
			}
			t.Run(variant.name+"/"+label, func(t *testing.T) {
				l, _ := newLuaWithPID(t)
				require.NoError(t, secapi.SetActor(l.Context(), secapi.Actor{ID: "caller"}))
				require.NoError(t, secapi.SetScope(l.Context(), secsystem.NewScope([]secapi.Policy{&spawnHostPolicy{allowHost: allowed}})))
				l.Push(lua.LString("app:worker"))
				l.Push(lua.LString("app:processes"))
				result := variant.call(l)
				if allowed {
					require.Equal(t, -1, result)
					yield, ok := l.Get(-1).(*SpawnYield)
					require.True(t, ok)
					yield.Release()
					return
				}
				// Denial must happen before any scheduler command can escape.
				require.Equal(t, 2, result)
				require.Equal(t, lua.LNil, l.Get(-2))
				err, ok := l.Get(-1).(*lua.Error)
				require.True(t, ok)
				require.Equal(t, lua.PermissionDenied, err.Kind())
			})
		}
	}
}
