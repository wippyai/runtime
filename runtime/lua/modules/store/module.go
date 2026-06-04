// SPDX-License-Identifier: MPL-2.0

package store

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	storeapi "github.com/wippyai/runtime/api/store"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const storeTypeName = "store.Store"

var (
	moduleTable *lua.LTable
	initOnce    sync.Once
)

func initModuleTable() {
	mod := lua.CreateTable(0, 3)
	mod.RawSetString("get", lua.LGoFunc(storeGet))

	backend := lua.CreateTable(0, 5)
	backend.RawSetString("KV_RAFT", lua.LString(storeapi.BackendKVRaft))
	backend.RawSetString("KV_CRDT", lua.LString(storeapi.BackendKVCRDT))
	backend.RawSetString("MEMORY", lua.LString(storeapi.BackendMemory))
	backend.RawSetString("SQL", lua.LString(storeapi.BackendSQL))
	backend.RawSetString("UNKNOWN", lua.LString(storeapi.BackendUnknown))
	backend.Immutable = true
	mod.RawSetString("backend", backend)

	consistency := lua.CreateTable(0, 4)
	consistency.RawSetString("LINEARIZABLE", lua.LString(storeapi.ConsistencyLinearizable))
	consistency.RawSetString("EVENTUAL", lua.LString(storeapi.ConsistencyEventual))
	consistency.RawSetString("LOCAL", lua.LString(storeapi.ConsistencyLocal))
	consistency.RawSetString("UNKNOWN", lua.LString(storeapi.ConsistencyUnknown))
	consistency.Immutable = true
	mod.RawSetString("consistency", consistency)

	mod.Immutable = true
	moduleTable = mod
}

func init() {
	value.RegisterTypeMethods(nil, storeTypeName,
		map[string]lua.LGoFunc{"__tostring": storeToString},
		storeMethods)
}

// Module is the store module definition.
var Module = &luaapi.ModuleDef{
	Name:        "store",
	Description: "Key-value store operations",
	Class:       []string{luaapi.ClassStorage, luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		initOnce.Do(initModuleTable)
		return moduleTable, []luaapi.YieldType{
			{Sample: &GetYield{}, CmdID: storeapi.Get},
			{Sample: &SetYield{}, CmdID: storeapi.Set},
			{Sample: &DeleteYield{}, CmdID: storeapi.Delete},
			{Sample: &HasYield{}, CmdID: storeapi.Has},
			{Sample: &EntryYield{}, CmdID: storeapi.EntryCommand},
			{Sample: &ListYield{}, CmdID: storeapi.ListCommand},
			{Sample: &PutYield{}, CmdID: storeapi.PutCommand},
		}
	},
	Types: ModuleTypes,
}
