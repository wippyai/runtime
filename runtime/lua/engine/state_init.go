package engine

import (
	"fmt"
	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/runtime/lua/engine/errors"
	"github.com/ponyruntime/pony/runtime/lua/engine/loadlib"
	lua "github.com/yuin/gopher-lua"
	"strings"
)

var SharedState *lua.LState

func init() {
	SharedState, _ = newLuaState()
	errors.RegisterErrorsModule(SharedState)
}

// CoreLib represents a core Lua library
type CoreLib struct {
	name string
	fn   lua.LGFunction
}

// coreLuaLibs defines the core Lua libraries to load

func getCoreLibs() []CoreLib {
	return []CoreLib{
		{lua.LoadLibName, loadlib.OpenRestrictedPackage},
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
		{lua.DebugLibName, lua.OpenDebug},
		{lua.CoroutineLibName, lua.OpenCoroutine},
		// never os or io
	}
}

// loadCoreLuaLibs loads the core Lua libraries into the State
func loadCoreLuaLibs(state *lua.LState) error {
	for _, lib := range getCoreLibs() {
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
		SkipOpenLibs:    true,
		RegistryMaxSize: 256 * 200,
		// todo: other options can be exposed later
		MinimizeStackMemory: true,
	})

	if err := loadCoreLuaLibs(state); err != nil {
		state.Close()
		return nil, err
	}

	// always redirect print to log, todo: move it somewhere?
	state.SetGlobal("print", state.NewFunction(func(L *lua.LState) int {
		log := logs.GetLogger(L.Context())

		// Build message by concatenating all arguments with spaces
		parts := make([]string, L.GetTop())
		for i := 1; i <= L.GetTop(); i++ {
			parts[i-1] = L.ToString(i)
		}
		msg := strings.Join(parts, " ")

		if log == nil {
			fmt.Print(msg)
			return 0
		}

		log.Info(msg)
		return 0
	}))

	return state, nil
}
