package store

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const storeTypeName = "store.Store"

var (
	moduleTable *lua.LTable
	initOnce    sync.Once
)

func initModuleTable() {
	mod := lua.CreateTable(0, 1)
	mod.RawSetString("get", lua.LGoFunc(storeGet))
	mod.Immutable = true
	moduleTable = mod
}

func init() {
	value.RegisterTypeMethods(nil, storeTypeName,
		map[string]lua.LGFunction{"__tostring": storeToString},
		storeMethods)
}

// Module is the store module definition.
var Module = &luaapi.ModuleDef{
	Name:        "store",
	Description: "Key-value store operations",
	Class:       []string{luaapi.ClassStorage, luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		initOnce.Do(initModuleTable)
		return moduleTable, nil
	},
}
