package store

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const storeTypeName = "store.Store"

var (
	moduleTable    *lua.LTable
	registration   *lua2api.Registration
	storeMetatable *lua.LTable
	initOnce       sync.Once
)

// Module is the singleton store module instance.
var Module = &storeModule{}

type storeModule struct{}

func (m *storeModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "store",
		Description: "Key-value store operations",
		Class:       []string{luaapi.ClassStorage, luaapi.ClassIO, luaapi.ClassNondeterministic},
	}
}

func (m *storeModule) Register(l *lua.LState) *lua2api.Registration {
	initOnce.Do(func() {
		moduleTable = createModuleTable()
		storeMetatable = value.RegisterTypeMethods(nil, storeTypeName,
			map[string]lua.LGFunction{"__tostring": storeToString},
			storeMethods)
		registration = &lua2api.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	return registration
}

func (m *storeModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use lua2api.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	lua2api.LoadModule(l, Module)
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 1)
	mod.RawSetString("get", lua.LGoFunc(storeGet))
	mod.Immutable = true
	return mod
}
