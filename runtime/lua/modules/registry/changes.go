// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"fmt"

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
	if !authorizeSnapshotRead(l, changes.snapshot) {
		return 2
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

	// Root-ness is deployment state, not entry content. An echoed-back root
	// value equal to the current selection passes; an attempted mutation is
	// refused and pointed at the API that owns it.
	if rootVal, ok := entryTable.RawGetString("root").(lua.LBool); ok {
		current := false
		if reg := regapi.GetRegistry(l.Context()); reg != nil {
			if reader, readerOK := reg.(provenanceReader); readerOK {
				if p, found := reader.EntryProvenance(entry.ID.Canonical()); found {
					current = p.Root
				}
			}
		}
		if bool(rootVal) != current {
			err := lua.NewLuaError(l, "root is deployment state; change it with registry.set_root").
				WithKind(lua.Invalid).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}
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

	ids, parseErr := deleteIDs(l.Get(2))
	if parseErr != nil {
		err := lua.WrapErrorWithLua(l, parseErr, "parse registry entry IDs").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}
	seen := make(map[regapi.ID]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		changes.ops = append(changes.ops, regapi.Operation{
			Kind:  regapi.EntryDelete,
			Entry: regapi.Entry{ID: id},
		})
	}

	l.Push(l.Get(1))
	return 1
}

func deleteIDs(value lua.LValue) ([]regapi.ID, error) {
	// Walk iteratively: Lua tables are graphs and may contain themselves. A
	// recursive decoder lets an untrusted self-reference exhaust the Go stack.
	stack := []lua.LValue{value}
	seenTables := make(map[*lua.LTable]struct{})
	ids := make([]regapi.ID, 0, 1)
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		switch v := current.(type) {
		case lua.LString:
			ids = append(ids, regapi.ParseID(string(v)))
		case *lua.LTable:
			if _, seen := seenTables[v]; seen {
				return nil, fmt.Errorf("cyclic or repeated ID table")
			}
			seenTables[v] = struct{}{}
			if idValue := v.RawGetString("id"); idValue != lua.LNil {
				id, ok := idValue.(lua.LString)
				if !ok {
					return nil, fmt.Errorf("entry id must be a string, got %s", idValue.Type())
				}
				ids = append(ids, regapi.ParseID(string(id)))
				continue
			}
			ns := v.RawGetString("ns")
			name := v.RawGetString("name")
			if ns != lua.LNil || name != lua.LNil {
				nsString, nsOK := ns.(lua.LString)
				nameString, nameOK := name.(lua.LString)
				if !nsOK || !nameOK {
					return nil, fmt.Errorf("entry ns and name must both be strings")
				}
				ids = append(ids, regapi.NewID(string(nsString), string(nameString)))
				continue
			}
			if v.Len() == 0 {
				return nil, fmt.Errorf("empty ID list")
			}
			for i := v.Len(); i >= 1; i-- {
				stack = append(stack, v.RawGetInt(i))
			}
		default:
			return nil, fmt.Errorf("unsupported ID value %s", current.Type())
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("empty ID list")
	}
	return ids, nil
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

	if changes.snapshot.overlayOwner != "" {
		owner := changes.snapshot.overlayOwner
		if !security.IsAllowed(l.Context(), "registry.overlay.get", owner, nil) {
			err := lua.NewLuaError(l, "not allowed to read registry overlay: "+owner).
				WithKind(lua.PermissionDenied).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}
		if !security.IsAllowed(l.Context(), "registry.overlay.apply", owner, nil) {
			err := lua.NewLuaError(l, "not allowed to apply registry overlay: "+owner).
				WithKind(lua.PermissionDenied).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}
		for _, op := range changes.ops {
			kind := op.Entry.Kind
			if op.Kind == regapi.EntryUpdate || op.Kind == regapi.EntryDelete {
				stored, getErr := changes.snapshot.GetEntry(op.Entry.ID)
				if getErr != nil {
					l.Push(lua.LNil)
					l.Push(lua.NewLuaError(l, "registry overlay entry not found: "+op.Entry.ID.String()).
						WithKind(lua.NotFound).
						WithRetryable(false).
						WithDetails(map[string]any{"entry_id": op.Entry.ID.String(), "owner": owner}))
					return 2
				}
				kind = stored.Kind
			}
			verb := "unknown"
			switch op.Kind {
			case regapi.EntryCreate:
				verb = "create"
			case regapi.EntryUpdate:
				verb = "update"
			case regapi.EntryDelete:
				verb = "delete"
			}
			action := "registry.overlay." + verb + "." + kind
			if !security.IsAllowed(l.Context(), action, op.Entry.ID.String(), nil) {
				l.Push(lua.LNil)
				l.Push(lua.NewLuaError(l, "not allowed to apply "+kind+" overlay entry: "+op.Entry.ID.String()).
					WithKind(lua.PermissionDenied).
					WithRetryable(false))
				return 2
			}
		}
		writer, ok := changes.snapshot.reg.(regapi.OverlayWriter)
		if !ok {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, "registry overlays are not supported").
				WithKind(lua.Internal).
				WithRetryable(false))
			return 2
		}
		if _, applyErr := writer.ApplyOverlay(l.Context(), owner, changes.snapshot.overlayGen, changes.ops); applyErr != nil {
			l.Push(lua.LNil)
			l.Push(lua.WrapErrorWithLua(l, applyErr, "apply registry overlay"))
			return 2
		}
		version, currentErr := changes.snapshot.reg.Current()
		if currentErr != nil {
			l.Push(lua.LNil)
			l.Push(lua.WrapErrorWithLua(l, currentErr, "get current registry version"))
			return 2
		}
		value.PushTypedUserData(l, version, typeVersion)
		l.Push(lua.LNil)
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
