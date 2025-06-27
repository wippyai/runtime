package engine

import (
	"fmt"
	"strings"

	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/runtime/lua/engine/errors"
	"github.com/ponyruntime/pony/runtime/lua/engine/loadlib"
	lua "github.com/yuin/gopher-lua"
)

var (
	SharedState     *lua.LState
	sharedPrintFunc *lua.LFunction
)

func init() {
	// Used to get env for global shared functions, todo: ensure it's mounted in every context
	SharedState, _ = newLuaState()

	// Create the print function once using SharedState - this function is stateless
	// and safe to reuse across all LStates since it only uses the LState passed to it
	sharedPrintFunc = SharedState.NewFunction(func(L *lua.LState) int {
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
	})
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
	// todo: other options can be exposed later
	state := lua.NewState(lua.Options{
		RegistrySize:        256,
		RegistryMaxSize:     256 * 256,
		RegistryGrowStep:    16,
		SkipOpenLibs:        true,
		CallStackSize:       128,
		MinimizeStackMemory: true,
	})

	errors.RegisterErrorsModule(state)

	if err := loadCoreLuaLibs(state); err != nil {
		state.Close()
		return nil, err
	}

	// Use the shared print function created once during init
	// This is safe because the function only uses the LState parameter passed to it
	state.SetGlobal("print", sharedPrintFunc)

	return state, nil
}
