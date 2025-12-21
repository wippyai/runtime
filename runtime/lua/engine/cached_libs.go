package engine

import (
	"sync"

	lua "github.com/yuin/gopher-lua"
)

var (
	initOnce sync.Once

	cachedTableLib     *lua.LTable
	cachedMathLib      *lua.LTable
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
	L.SetGlobal(lua.CoroutineLibName, cachedCoroutineLib)
	L.SetGlobal(lua.StringLibName, cachedStringLib)
	L.SetGlobal(lua.ErrorsLibName, cachedErrorsLib)

	// Register error metatable (needed per-LState for builtinMts)
	lua.RegisterErrorMetatable(L)

	// String lib as metatable for string type
	L.SetMetatable(lua.LString(""), cachedStringLib)
}
