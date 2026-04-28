// SPDX-License-Identifier: MPL-2.0

package registry

import (
	lua "github.com/wippyai/go-lua"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/security"
	"go.uber.org/zap"
)

// Changes represents a set of operations to modify the registry
type Changes struct {
	snapshot *Snapshot
	log      *zap.Logger
	ops      []regapi.Operation
}

// changesOps returns the operations in a changeset
func changesOps(l *lua.LState) int {
	changes := checkChanges(l)
	if changes == nil {
		return 0
	}

	opsTable := l.CreateTable(len(changes.ops), 0)

	for i, op := range changes.ops {
		opTable := l.CreateTable(0, 2)
		opTable.RawSetString("kind", lua.LString(op.Kind))

		entryTable, convErr := entryToLuaTable(l, op.Entry)
		if convErr != nil {
			err := lua.WrapErrorWithLua(l, convErr, "convert entry").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		opTable.RawSetString("entry", entryTable)
		opsTable.RawSetInt(i+1, opTable)
	}

	l.Push(opsTable)
	return 1
}

// changesCreate adds a new entry to the changeset
func changesCreate(l *lua.LState) int {
	changes := checkChanges(l)
	if changes == nil {
		return 0
	}

	entryTable := l.CheckTable(2)

	entry, convErr := luaTableToEntry(l, entryTable)
	if convErr != nil {
		err := lua.WrapErrorWithLua(l, convErr, "convert entry").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	changes.ops = append(changes.ops, regapi.Operation{
		Kind:  regapi.EntryCreate,
		Entry: entry,
	})

	l.Push(l.Get(1))
	return 1
}

// changesUpdate adds an entry update to the changeset
func changesUpdate(l *lua.LState) int {
	changes := checkChanges(l)
	if changes == nil {
		return 0
	}

	entryTable := l.CheckTable(2)

	entry, convErr := luaTableToEntry(l, entryTable)
	if convErr != nil {
		err := lua.WrapErrorWithLua(l, convErr, "convert entry").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	changes.ops = append(changes.ops, regapi.Operation{
		Kind:  regapi.EntryUpdate,
		Entry: entry,
	})

	l.Push(l.Get(1))
	return 1
}

// changesDelete adds an entry deletion to the changeset
func changesDelete(l *lua.LState) int {
	changes := checkChanges(l)
	if changes == nil {
		return 0
	}

	idVal := l.Get(2)
	var id regapi.ID

	switch v := idVal.(type) {
	case lua.LString:
		id = regapi.ParseID(string(v))
	case *lua.LTable:
		ns := v.RawGetString("ns")
		name := v.RawGetString("name")
		id = regapi.NewID(ns.String(), name.String())
	default:
		err := lua.NewLuaError(l, "invalid ID format").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	changes.ops = append(changes.ops, regapi.Operation{
		Kind: regapi.EntryDelete,
		Entry: regapi.Entry{
			ID: id,
		},
	})

	l.Push(l.Get(1))
	return 1
}

// changesApply applies the changeset to create a new version
func changesApply(l *lua.LState) int {
	changes := checkChanges(l)
	if changes == nil {
		return 0
	}

	if len(changes.ops) == 0 {
		err := lua.NewLuaError(l, "no changes to apply").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	if !security.IsAllowed(l.Context(), "registry.apply", "", nil) {
		err := lua.NewLuaError(l, "not allowed to apply registry changes").
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	version, applyErr := changes.snapshot.reg.Apply(l.Context(), changes.ops)
	if applyErr != nil {
		err := lua.WrapErrorWithLua(l, applyErr, "apply changes").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	value.PushTypedUserData(l, version, typeVersion)
	l.Push(lua.LNil)
	return 2
}

// checkChanges checks if the first argument is a Changes userdata
func checkChanges(l *lua.LState) *Changes {
	ud := l.CheckUserData(1)
	if changes, ok := ud.Value.(*Changes); ok {
		return changes
	}
	l.ArgError(1, "changes expected")
	return nil
}
