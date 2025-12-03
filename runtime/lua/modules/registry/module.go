package registry

import (
	"errors"
	"sync"

	regapi "github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

const (
	snapshotMetatable = "registry.Snapshot"
	changesMetatable  = "registry.Changes"
	versionMetatable  = "registry.Version"
	historyMetatable  = "registry.History"
)

var (
	moduleTable  *lua.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
)

// wrapVersion wraps a registry.Version in a Lua userdata.
func wrapVersion(l *lua.LState, version regapi.Version) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = version
	ud.Metatable = value.GetTypeMetatable(l, versionMetatable)
	return ud
}

// tableToID converts a Lua table to a registry ID.
func tableToID(_ *lua.LState, table *lua.LTable) (regapi.ID, error) {
	ns := table.RawGetString("ns")
	name := table.RawGetString("name")

	if ns == lua.LNil || name == lua.LNil {
		return regapi.ID{}, errors.New("id table must have ns and name fields")
	}

	return regapi.ID{
		NS:   ns.String(),
		Name: name.String(),
	}, nil
}

// Module is the singleton registry module instance.
var Module = &registryModule{}

type registryModule struct{}

func (m *registryModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "registry",
		Description: "Registry get and find operations",
		Class:       []string{luaapi.ClassNondeterministic},
	}
}

func (m *registryModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		moduleTable = createModuleTable()
		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	return registration
}

func (m *registryModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 5)

	mod.RawSetString("get", lua.LGoFunc(registryGet))
	mod.RawSetString("find", lua.LGoFunc(registryFind))
	mod.RawSetString("parse_id", lua.LGoFunc(parseID))

	mod.Immutable = true
	return mod
}

func parseID(l *lua.LState) int {
	idStr := l.CheckString(1)
	id := regapi.ParseID(idStr)

	idTable := l.CreateTable(0, 2)
	idTable.RawSetString("ns", lua.LString(id.NS))
	idTable.RawSetString("name", lua.LString(id.Name))

	l.Push(idTable)
	return 1
}

func registryGet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("registry not found in context"))
		return 2
	}

	idStr := l.CheckString(1)
	id := regapi.ParseID(idStr)

	if !security.IsAllowed(ctx, "registry.get", id.String(), nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("registry get not allowed for " + id.String()))
		return 2
	}

	entry, err := reg.GetEntry(id)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	entryTable := simpleEntryToLuaTable(l, entry)
	l.Push(entryTable)
	l.Push(lua.LNil)
	return 2
}

func registryFind(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("registry not found in context"))
		return 2
	}

	filterTable := l.CheckTable(1)
	if filterTable == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("filter criteria table required"))
		return 2
	}

	meta := simpleConvertFilterToMetadata(filterTable)

	finder := regapi.GetFinder(ctx)
	if finder == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("finder not available in context"))
		return 2
	}

	entries, err := finder.Find(meta)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	entriesTable := l.CreateTable(len(entries), 0)
	for _, entry := range entries {
		if !security.IsAllowed(ctx, "registry.get", entry.ID.String(), nil) {
			continue
		}
		entryTable := simpleEntryToLuaTable(l, entry)
		entriesTable.Append(entryTable)
	}

	l.Push(entriesTable)
	l.Push(lua.LNil)
	return 2
}

func simpleEntryToLuaTable(l *lua.LState, entry regapi.Entry) *lua.LTable {
	t := l.CreateTable(0, 4)

	// Use string format for ID to maintain backward compatibility
	t.RawSetString("id", lua.LString(entry.ID.String()))

	t.RawSetString("kind", lua.LString(entry.Kind))

	if entry.Meta != nil {
		metaTable := l.CreateTable(0, len(entry.Meta))
		for k, v := range entry.Meta {
			metaTable.RawSetString(k, toLuaValue(l, v))
		}
		t.RawSetString("meta", metaTable)
	}

	if entry.Data != nil {
		t.RawSetString("data", toLuaValue(l, entry.Data.Data()))
	}

	return t
}

func simpleConvertFilterToMetadata(filterTable *lua.LTable) regapi.Metadata {
	meta := regapi.Metadata{}
	filterTable.ForEach(func(k, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			meta[string(ks)] = toGoValue(v)
		}
	})
	return meta
}

func toLuaValue(l *lua.LState, val any) lua.LValue {
	if val == nil {
		return lua.LNil
	}
	switch v := val.(type) {
	case string:
		return lua.LString(v)
	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)
	case float64:
		return lua.LNumber(v)
	case bool:
		return lua.LBool(v)
	case []byte:
		return lua.LString(v)
	case map[string]any:
		t := l.CreateTable(0, len(v))
		for k, val := range v {
			t.RawSetString(k, toLuaValue(l, val))
		}
		return t
	case []any:
		t := l.CreateTable(len(v), 0)
		for i, val := range v {
			t.RawSetInt(i+1, toLuaValue(l, val))
		}
		return t
	default:
		return lua.LNil
	}
}

func toGoValue(lv lua.LValue) any {
	switch v := lv.(type) {
	case lua.LString:
		return string(v)
	case lua.LNumber:
		return float64(v)
	case lua.LInteger:
		return int64(v)
	case lua.LBool:
		return bool(v)
	case *lua.LNilType:
		return nil
	case *lua.LTable:
		m := make(map[string]any)
		v.ForEach(func(k, val lua.LValue) {
			if ks, ok := k.(lua.LString); ok {
				m[string(ks)] = toGoValue(val)
			}
		})
		return m
	default:
		return nil
	}
}
