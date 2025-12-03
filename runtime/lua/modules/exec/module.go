package exec

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const (
	executorTypeName = "exec.Executor"
	processTypeName  = "exec.Process"
)

var (
	moduleTable       *lua.LTable
	registration      *luaapi.Registration
	executorMetatable *lua.LTable
	processMetatable  *lua.LTable
	initOnce          sync.Once
)

// Module is the singleton exec module instance.
var Module = &execModule{}

type execModule struct{}

func (m *execModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "exec",
		Description: "Command execution and process management",
		Class:       []string{luaapi.ClassIO, luaapi.ClassProcess, luaapi.ClassNondeterministic},
	}
}

func (m *execModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		moduleTable = createModuleTable()
		executorMetatable = value.RegisterTypeMethods(nil, executorTypeName,
			map[string]lua.LGFunction{"__tostring": executorToString},
			executorMethods)
		processMetatable = value.RegisterTypeMethods(nil, processTypeName,
			map[string]lua.LGFunction{"__tostring": processToString},
			processMethods)
		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	return registration
}

func (m *execModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 1)
	mod.RawSetString("get", lua.LGoFunc(execGet))
	mod.Immutable = true
	return mod
}
