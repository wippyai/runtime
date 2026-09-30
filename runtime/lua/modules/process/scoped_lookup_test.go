// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/runtime/lua/code"
	systemtopology "github.com/wippyai/runtime/system/topology"
)

func TestRegistryLookup_ExactScope(t *testing.T) {
	local := systemtopology.NewPIDRegistry()
	localPID := pid.PID{Node: "local", Host: "app", UniqID: "local"}
	eventualPID := pid.PID{Node: "eventual", Host: "app", UniqID: "eventual"}
	globalPID := pid.PID{Node: "global", Host: "app", UniqID: "global"}
	_, err := local.Register("service", localPID)
	require.NoError(t, err)
	eventual := &fakeEventualRegistry{entries: map[string]pid.PID{"service": eventualPID}}
	local.SetEventualRegistry(eventual)
	global := newFakeScopedRegistry()
	_, err = global.Register(context.Background(), "service", globalPID)
	require.NoError(t, err)
	l, _ := newLuaWithPIDAndRegistry(t, local)
	topology.WithEventualRegistry(l.Context(), eventual)
	globalapi.WithRegistry(l.Context(), global)
	for _, tc := range []struct {
		scope string
		want  pid.PID
	}{
		{"nil", globalPID},
		{"process.registry.LOCAL", localPID},
		{"process.registry.EVENTUAL", eventualPID},
		{"process.registry.CONSISTENT", globalPID},
		{"process.registry.STRONG", globalPID},
	} {
		require.NoError(t, l.DoString(fmt.Sprintf(`
            local owner, err = process.registry.lookup("service", %s)
            assert(owner == %q, tostring(err))
        `, tc.scope, tc.want.String())), tc.scope)
	}
}

func TestRegistryLookup_ExactScopeDoesNotFallThrough(t *testing.T) {
	for _, scope := range []string{"LOCAL", "EVENTUAL", "CONSISTENT", "STRONG"} {
		for _, failure := range []string{"absent name", "absent registry", "registry error"} {
			if scope == "LOCAL" && failure == "registry error" {
				continue // Local-only table reads have no remote backend errors.
			}
			t.Run(scope+"/"+failure, func(t *testing.T) {
				local := systemtopology.NewPIDRegistry()
				fallback := pid.PID{Node: "node", Host: "app", UniqID: "fallback"}
				_, err := local.Register("service", fallback)
				require.NoError(t, err)
				eventual := &fakeEventualRegistry{entries: map[string]pid.PID{"service": fallback}}
				global := newFakeScopedRegistry()
				_, err = global.Register(context.Background(), "service", fallback)
				require.NoError(t, err)
				l, _ := newLuaWithPIDAndRegistry(t, local)
				wantKind := "NotFound"
				if failure != "absent name" {
					wantKind = "Unavailable"
				}
				switch scope {
				case "LOCAL":
					if failure == "absent name" {
						local.Unregister("service")
					} else {
						l, _ = newLuaWithPID(t)
					}
					local.SetEventualRegistry(eventual)
				case "EVENTUAL":
					if failure == "absent name" {
						eventual.entries = nil
					} else if failure == "registry error" {
						eventual.lookupErr = errors.New("selected registry failed")
						wantKind = "Internal"
					}
				default:
					if failure == "absent name" {
						_, err = global.Unregister(context.Background(), "service")
						require.NoError(t, err)
					} else if failure == "registry error" {
						global.lookupErr = errors.New("selected registry failed")
						wantKind = "Internal"
					}
				}
				if !(scope == "EVENTUAL" && failure == "absent registry") {
					topology.WithEventualRegistry(l.Context(), eventual)
				}
				if !((scope == "CONSISTENT" || scope == "STRONG") && failure == "absent registry") {
					globalapi.WithRegistry(l.Context(), global)
				}
				require.NoError(t, l.DoString(fmt.Sprintf(`
                    local owner, err = process.registry.lookup("service", process.registry.%s)
                    assert(owner == nil, "must not resolve another scope")
                    assert(err ~= nil and err:kind() == %q, tostring(err))
                `, scope, wantKind)))
			})
		}
	}
}

func TestRegistryLookup_ExactScopeInvalidSelector(t *testing.T) {
	l, _ := newLuaWithPIDAndRegistry(t, systemtopology.NewPIDRegistry())
	for _, selector := range []string{`"LOCAL"`, "{}", "true", "-1", "4", "0.5", "0/0", "math.huge"} {
		require.NoError(t, l.DoString(fmt.Sprintf(`
            local owner, err = process.registry.lookup("service", %s)
            assert(owner == nil)
            assert(err ~= nil and err:kind() == "Invalid", tostring(err))
        `, selector)), selector)
	}
}

func TestRegistryLookup_ExactScopeCancellation(t *testing.T) {
	local := systemtopology.NewPIDRegistry()
	_, err := local.Register("service", pid.PID{Host: "app", UniqID: "owner"})
	require.NoError(t, err)
	l, _ := newLuaWithPIDAndRegistry(t, local)
	ctx, cancel := context.WithCancel(l.Context())
	cancel()
	l.SetContext(ctx)
	// Call the module directly: the VM itself refuses to execute a canceled chunk.
	l.Push(lua.LString("service"))
	l.Push(lua.LNumber(0))
	require.Equal(t, 2, registryLookup(l))
	require.Contains(t, l.Get(-1).String(), "context canceled")
}

func TestRegistryLookup_ExactLocalRejectsComposedOnlyRegistry(t *testing.T) {
	l, _ := newLuaWithPIDAndRegistry(t, &fakePIDRegistry{entries: map[string]pid.PID{
		"service": {Host: "app", UniqID: "not-proven-local"},
	}})
	require.NoError(t, l.DoString(`
        local owner, err = process.registry.lookup("service", process.registry.LOCAL)
        assert(owner == nil)
        assert(err ~= nil and err:kind() == "Unavailable", tostring(err))
    `))
}

func TestRegistryLookup_ExactLocalDoesNotConsultParent(t *testing.T) {
	parent := systemtopology.NewPIDRegistry()
	owner := pid.PID{Host: "app", UniqID: "parent"}
	_, err := parent.Register("service", owner)
	require.NoError(t, err)
	local := systemtopology.NewPIDRegistry(systemtopology.WithParent(parent))
	l, _ := newLuaWithPIDAndRegistry(t, local)
	require.NoError(t, l.DoString(fmt.Sprintf(`
        assert(process.registry.lookup("service") == %q)
        local owner, err = process.registry.lookup("service", process.registry.LOCAL)
        assert(owner == nil)
        assert(err ~= nil and err:kind() == "NotFound", tostring(err))
    `, owner.String())))
}

func TestRegistryLookup_ExactScopeTypeManifest(t *testing.T) {
	for _, selector := range []string{"", ", nil", ", process.registry.LOCAL", ", process.registry.STRONG", `, "LOCAL"`} {
		tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
		_, diagnostics, err := tc.Check(`
local process = require("process")
local owner, err = process.registry.lookup("service"`+selector+`)
`, "scoped_lookup.lua", map[string]*io.Manifest{"process": ModuleTypes()})
		require.NoError(t, err)
		require.Equal(t, selector == `, "LOCAL"`, code.HasErrors(diagnostics), "diagnostics: %v", diagnostics)
	}
}
