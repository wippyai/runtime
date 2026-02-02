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
)

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
	})
}

// BindCachedLibs binds all cached libs to an LState
func BindCachedLibs(L *lua.LState) {
	initCachedLibs()

	L.SetGlobal(lua.TabLibName, cachedTableLib)
	L.SetGlobal(lua.MathLibName, cachedMathLib)
	L.SetGlobal("os", cachedOsLib)
	L.SetGlobal(lua.CoroutineLibName, cachedCoroutineLib)
	L.SetGlobal(lua.ErrorsLibName, cachedErrorsLib)

	// Register error metatable (needed per-LState for builtinMts)
	lua.RegisterErrorMetatable(L)

	// Register primitive type globals for typecasting via LType singletons.
	// LType values are callable via VM's native typeCall dispatch.
	// string(x), integer(x), number(x), boolean(x) all use VM fast path.
	L.SetGlobal("string", lua.LTypeString)
	L.SetGlobal("integer", lua.LTypeInteger)
	L.SetGlobal("number", lua.LTypeNumber)
	L.SetGlobal("boolean", lua.LTypeBoolean)

	// String lib as metatable for string values (enables "hello":upper())
	L.SetMetatable(lua.LString(""), cachedStringLib)
}
