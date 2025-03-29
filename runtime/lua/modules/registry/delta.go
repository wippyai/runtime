package registry

import (
	"fmt"
	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/registry/topology"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// buildDelta creates a changeset for transitioning from one state to another
func (m *Module) buildDelta(l *lua.LState) int {
	// Get the "from" entries
	fromTable := l.CheckTable(1)
	if fromTable == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("from_entries table required"))
		return 2
	}

	// Get the "to" entries
	toTable := l.CheckTable(2)
	if toTable == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("to_entries table required"))
		return 2
	}

	// Convert Lua tables to Go registry.State
	fromEntries := make(regapi.State, 0)
	toEntries := make(regapi.State, 0)

	fromTable.ForEach(func(_, v lua.LValue) {
		if entryTable, ok := v.(*lua.LTable); ok {
			entry, err := luaTableToEntry(l, entryTable)
			if err == nil {
				fromEntries = append(fromEntries, entry)
			} else {
				m.log.Debug("error converting entry", zap.Error(err))
			}
		}
	})

	toTable.ForEach(func(_, v lua.LValue) {
		if entryTable, ok := v.(*lua.LTable); ok {
			entry, err := luaTableToEntry(l, entryTable)
			if err == nil {
				toEntries = append(toEntries, entry)
			} else {
				m.log.Debug("error converting entry", zap.Error(err))
			}
		}
	})

	// Create state builder
	stateBuilder := topology.NewStateBuilder(m.log)

	// Build delta between states
	changeSet, err := stateBuilder.BuildDelta(fromEntries, toEntries)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Convert changeSet to Lua table
	resultTable := l.NewTable()
	for i, op := range changeSet {
		opTable := l.NewTable()
		opTable.RawSetString("kind", lua.LString(op.Kind))

		entryTable, err := entryToLuaTable(l, op.Entry)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("failed to convert entry: %v", err)))
			return 2
		}

		opTable.RawSetString("entry", entryTable)
		resultTable.RawSetInt(i+1, opTable)
	}

	l.Push(resultTable)
	l.Push(lua.LNil)
	return 2
}
