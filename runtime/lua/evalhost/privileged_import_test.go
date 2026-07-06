// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"go.uber.org/zap"
)

// phoneModule stands in for a privileged capability ("phone calls") that an
// eval'd program must not reach directly, only through a granted import.
// phone carries a forbidden class (network), so an eval'd program can never get
// it by default — it is reachable only when explicitly granted to an import.
var phoneModule = &luaapi.ModuleDef{
	Name:        "phone",
	Description: "Privileged capability for testing import grants",
	Class:       []string{luaapi.ClassNetwork},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		t := lua.CreateTable(0, 1)
		t.RawSetString("call", lua.LGoFunc(func(s *lua.LState) int {
			s.Push(lua.LString("ringing"))
			return 1
		}))
		return t, nil
	},
}

func phoneModulesProvider() ModuleProvider {
	return func() []*luaapi.ModuleDef {
		return []*luaapi.ModuleDef{json.Module, timemod.Module, phoneModule}
	}
}

// quoteLibrarySource is a privileged import: it uses the phone capability
// internally and exposes only a narrow API.
const quoteLibrarySource = `
local phone = require("phone")
local quote = {}
function quote.place_call()
    return phone.call()
end
function quote.sees_phone()
    return type(phone)
end
return quote
`

// TestHost_Run_PrivilegedImport_GrantsModuleToImportOnly proves that a module
// granted to an import is usable inside the import's code but unreachable from
// the eval'd program that imported it.
func TestHost_Run_PrivilegedImport_GrantsModuleToImportOnly(t *testing.T) {
	host := NewHost(zap.NewNop(), phoneModulesProvider())
	libID := registry.ParseID("test.lib:quote")
	host.WithImportLoader(mockImportLoader(map[registry.ID]string{
		libID: quoteLibrarySource,
	}))

	// The eval'd program declares no modules; it only imports the library,
	// granting that library the phone capability.
	runWith := func(t *testing.T, source, method string) (any, error) {
		t.Helper()
		return host.Run(context.Background(), RunCmd{
			Source:  source,
			Method:  method,
			Modules: []string{},
			Imports: map[string]registry.ID{"quote": libID},
			ImportModules: map[string][]string{
				"quote": {"phone"},
			},
		})
	}

	t.Run("import can use the granted capability", func(t *testing.T) {
		result, err := runWith(t, `
			return { run = function() return quote.place_call() end }
		`, "run")
		require.NoError(t, err)
		lstr, ok := result.(lua.LString)
		require.True(t, ok, "result should be LString, got %T", result)
		assert.Equal(t, "ringing", string(lstr))
	})

	t.Run("import really sees the capability table", func(t *testing.T) {
		result, err := runWith(t, `
			return { run = function() return quote.sees_phone() end }
		`, "run")
		require.NoError(t, err)
		lstr, ok := result.(lua.LString)
		require.True(t, ok, "result should be LString, got %T", result)
		assert.Equal(t, "table", string(lstr))
	})

	t.Run("eval'd program cannot see the capability as a global", func(t *testing.T) {
		result, err := runWith(t, `
			return { run = function() return type(phone) end }
		`, "run")
		require.NoError(t, err)
		lstr, ok := result.(lua.LString)
		require.True(t, ok, "result should be LString, got %T", result)
		assert.Equal(t, "nil", string(lstr), "phone must not leak into the eval program globals")
	})

	t.Run("eval'd program cannot require the capability", func(t *testing.T) {
		result, err := runWith(t, `
			return { run = function()
				local ok = pcall(require, "phone")
				return ok
			end }
		`, "run")
		require.NoError(t, err)
		lbool, ok := result.(lua.LBool)
		require.True(t, ok, "result should be LBool, got %T", result)
		assert.Equal(t, lua.LBool(false), lbool, "require('phone') must fail in the eval program")
	})
}
