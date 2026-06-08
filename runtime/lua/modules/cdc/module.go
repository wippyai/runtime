// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
)

var Module = &luaapi.ModuleDef{
	Name:        "cdc",
	Description: "Postgres CDC source introspection",
	Class:       []string{luaapi.ClassStorage, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		mod := lua.CreateTable(0, 2)
		mod.RawSetString("list_sources", lua.LGoFunc(listSources))
		mod.RawSetString("source", lua.LGoFunc(getSource))
		mod.Immutable = true
		return mod, nil
	},
	Types: ModuleTypes,
}

func listSources(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false))
		return 2
	}

	inspector := cdcapi.GetSourceInspector(ctx)
	if inspector == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "cdc source inspector not found").
			WithKind(lua.Internal).
			WithRetryable(false))
		return 2
	}

	infos := inspector.List()
	result := l.CreateTable(len(infos), 0)
	for i, info := range infos {
		result.RawSetInt(i+1, sourceInfoToTable(l, info))
	}
	l.Push(result)
	l.Push(lua.LNil)
	return 2
}

func getSource(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false))
		return 2
	}

	name := l.CheckString(1)
	if name == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "source name is required").
			WithKind(lua.Invalid).
			WithRetryable(false))
		return 2
	}

	inspector := cdcapi.GetSourceInspector(ctx)
	if inspector == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "cdc source inspector not found").
			WithKind(lua.Internal).
			WithRetryable(false))
		return 2
	}

	info, ok := inspector.Get(name)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	l.Push(sourceInfoToTable(l, info))
	l.Push(lua.LNil)
	return 2
}

func sourceInfoToTable(l *lua.LState, info cdcapi.SourceInfo) *lua.LTable {
	t := l.CreateTable(0, 9)
	t.RawSetString("name", lua.LString(info.Name))
	t.RawSetString("slot", lua.LString(info.Slot))
	t.RawSetString("event_system", lua.LString(info.EventSystem))
	if info.Publication != "" {
		t.RawSetString("publication", lua.LString(info.Publication))
	}
	if len(info.Tables) > 0 {
		tables := l.CreateTable(len(info.Tables), 0)
		for i, name := range info.Tables {
			tables.RawSetInt(i+1, lua.LString(name))
		}
		t.RawSetString("tables", tables)
	}
	t.RawSetString("streaming", lua.LBool(info.Streaming))
	t.RawSetString("failover", lua.LBool(info.Failover))
	t.RawSetString("temporary", lua.LBool(info.Temporary))
	t.RawSetString("snapshot", lua.LBool(info.Snapshot))
	return t
}
