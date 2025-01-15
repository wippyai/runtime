package engine

import (
	lua "github.com/yuin/gopher-lua"
)

// CoreLib represents a core Lua library
type CoreLib struct {
	name string
	fn   lua.LGFunction
}

// coreLuaLibs defines the core Lua libraries to load
var coreLuaLibs = []CoreLib{
	{lua.LoadLibName, lua.OpenPackage}, // Must be first
	{lua.BaseLibName, lua.OpenBase},
	{lua.TabLibName, lua.OpenTable},
	{lua.StringLibName, lua.OpenString},
	{lua.MathLibName, lua.OpenMath},
	{lua.DebugLibName, lua.OpenDebug},
	{lua.CoroutineLibName, lua.OpenCoroutine},
	// never os or io
}

// loadCoreLuaLibs loads the core Lua libraries into the State
func loadCoreLuaLibs(state *lua.LState) error {
	for _, lib := range coreLuaLibs {
		if err := state.CallByParam(lua.P{
			Fn:      state.NewFunction(lib.fn),
			NRet:    0,
			Protect: true,
		}, lua.LString(lib.name)); err != nil {
			return err
		}
	}
	return nil
}

// newLuaState creates a new Lua State with core libraries
func newLuaState() (*lua.LState, error) {
	state := lua.NewState(lua.Options{
		SkipOpenLibs: true,
	})

	if err := loadCoreLuaLibs(state); err != nil {
		state.Close()
		return nil, err
	}

	return state, nil
}
