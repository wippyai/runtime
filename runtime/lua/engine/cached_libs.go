// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/modules/ostime"
)

var (
	initOnce sync.Once

	cachedTableLib     *lua.LTable
	cachedMathLib      *lua.LTable
	cachedOsLib        *lua.LTable
	cachedCoroutineLib *lua.LTable
	cachedStringLib    *lua.LTable
	cachedErrorsLib    *lua.LTable

	// cachedLibs is the ordered set of standard libraries bound as globals by
	// BindCachedLibs. It is the single source for both binding and StdLibNames.
	cachedLibs []cachedLib
)

type cachedLib struct {
	name string
	tbl  *lua.LTable
}

func initCachedLibs() {
	initOnce.Do(func() {
		// Build libs once using temp state
		tmp := lua.NewState(lua.Options{SkipOpenLibs: true})
		defer tmp.Close()

		lua.OpenTable(tmp)
		cachedTableLib = tmp.GetGlobal(lua.TabLibName).(*lua.LTable)
		cachedTableLib.Immutable = true

		lua.OpenMath(tmp)
		cachedMathLib = tmp.GetGlobal(lua.MathLibName).(*lua.LTable)
		cachedMathLib.Immutable = true

		// Use wippy's ostime module for os.time/clock/date/difftime
		cachedOsLib, _ = ostime.Module.Build()

		lua.OpenCoroutine(tmp)
		cachedCoroutineLib = tmp.GetGlobal(lua.CoroutineLibName).(*lua.LTable)
		cachedCoroutineLib.Immutable = true

		lua.OpenString(tmp)
		cachedStringLib = tmp.GetGlobal(lua.StringLibName).(*lua.LTable)
		cachedStringLib.Immutable = true

		lua.OpenErrors(tmp)
		cachedErrorsLib = tmp.GetGlobal(lua.ErrorsLibName).(*lua.LTable)
		cachedErrorsLib.Immutable = true

		cachedLibs = []cachedLib{
			{lua.TabLibName, cachedTableLib},
			{lua.MathLibName, cachedMathLib},
			{"os", cachedOsLib},
			{lua.CoroutineLibName, cachedCoroutineLib},
			{lua.StringLibName, cachedStringLib},
			{lua.ErrorsLibName, cachedErrorsLib},
		}
	})
}

// BindCachedLibs binds all cached libs to an LState
func BindCachedLibs(L *lua.LState) {
	initCachedLibs()

	for _, lib := range cachedLibs {
		L.SetGlobal(lib.name, lib.tbl)
	}

	// Register error metatable (needed per-LState for builtinMts)
	lua.RegisterErrorMetatable(L)

	// String lib as metatable for string values (enables "hello":upper())
	L.SetMetatable(lua.LString(""), cachedStringLib)
}

// StdLibNames returns the names of the standard libraries BindCachedLibs
// installs as globals.
func StdLibNames() []string {
	initCachedLibs()

	names := make([]string, len(cachedLibs))
	for i, lib := range cachedLibs {
		names[i] = lib.name
	}
	return names
}
