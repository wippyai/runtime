// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"fmt"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/event"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/security"
	"go.uber.org/zap"
)

// Changes represents a set of operations to modify the registry
type Changes struct {
	snapshot *Snapshot
	log      *zap.Logger
	// plan is the reviewed plan a later apply is bound to. Any mutation of
	// ops drops it, since the reviewed changeset no longer exists.
	plan *regapi.Plan
	ops  []regapi.Operation
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

	changes.plan = nil
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

	changes.plan = nil
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
		changes.plan = nil
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
			shadow := false
			if op.Kind == regapi.EntryUpdate || op.Kind == regapi.EntryDelete {
				stored, getErr := changes.snapshot.GetEntry(op.Entry.ID)
				if getErr != nil {
					// An entry the overlay does not own is a durable entry the
					// operation shadows, and carries the durable kind.
					durable, durableErr := changes.snapshot.reg.GetEntry(op.Entry.ID)
					if durableErr != nil {
						l.Push(lua.LNil)
						l.Push(lua.NewLuaError(l, "registry overlay entry not found: "+op.Entry.ID.String()).
							WithKind(lua.NotFound).
							WithRetryable(false).
							WithDetails(map[string]any{"entry_id": op.Entry.ID.String(), "owner": owner}))
						return 2
					}
					stored, shadow = durable, true
				}
				kind = stored.Kind
			}
			action := "registry.overlay." + operationVerb(op.Kind) + "." + kind
			if !security.IsAllowed(l.Context(), action, op.Entry.ID.String(), nil) {
				l.Push(lua.LNil)
				l.Push(lua.NewLuaError(l, "not allowed to apply "+kind+" overlay entry: "+op.Entry.ID.String()).
					WithKind(lua.PermissionDenied).
					WithRetryable(false))
				return 2
			}
			if shadow && !security.IsAllowed(l.Context(), "registry.overlay.shadow", op.Entry.ID.String(), nil) {
				l.Push(lua.LNil)
				l.Push(lua.NewLuaError(l, "not allowed to shadow durable entry: "+op.Entry.ID.String()).
					WithKind(lua.PermissionDenied).
					WithRetryable(false).
					WithDetails(map[string]any{"entry_id": op.Entry.ID.String(), "owner": owner}))
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

	if denied := authorizeDurableChanges(l, changes); denied != nil {
		l.Push(lua.LNil)
		l.Push(denied)
		return 2
	}
	applier, ok := changes.snapshot.reg.(regapi.PlanApplier)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "registry cannot fence an apply on the snapshot version").
			WithKind(lua.Internal).
			WithRetryable(false))
		return 2
	}

	var (
		version  regapi.Version
		applyErr error
	)
	if changes.plan != nil {
		version, applyErr = applier.ApplyPlan(l.Context(), changes.plan)
	} else {
		version, applyErr = applier.ApplyAt(l.Context(), changes.snapshot.version, changes.ops)
	}
	if applyErr != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, applyErr, "apply changes"))
		return 2
	}
	changes.plan = nil

	value.PushTypedUserData(l, version, typeVersion)
	l.Push(lua.LNil)
	return 2
}

// authorizeDurableChanges evaluates registry.apply once per operation with the
// entry ID as the resource, the way registry.get is evaluated per entry. A
// policy granting registry.apply on every resource behaves as before; a policy
// scoped to a namespace pattern grants write authority over those entries only.
// One denied operation refuses the whole changeset.
func authorizeDurableChanges(l *lua.LState, changes *Changes) *lua.Error {
	for _, op := range changes.ops {
		if security.IsAllowed(l.Context(), "registry.apply", op.Entry.ID.String(), nil) {
			continue
		}
		return lua.NewLuaError(l, "not allowed to "+operationVerb(op.Kind)+" registry entry: "+op.Entry.ID.String()).
			WithKind(lua.PermissionDenied).
			WithRetryable(false).
			WithDetails(map[string]any{"entry_id": op.Entry.ID.String(), "action": "registry.apply"})
	}
	return nil
}

func operationVerb(kind event.Kind) string {
	switch kind {
	case regapi.EntryCreate:
		return "create"
	case regapi.EntryUpdate:
		return "update"
	case regapi.EntryDelete:
		return "delete"
	}
	return "unknown"
}

// changesPlan computes what applying the changeset would do without doing it.
// A successful plan binds the next apply to exactly what was reviewed.
func changesPlan(l *lua.LState) int {
	changes := checkChanges(l)
	if changes == nil {
		return 0
	}
	if changes.snapshot.overlayOwner != "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "plan requires a durable registry snapshot").
			WithKind(lua.Invalid).
			WithRetryable(false))
		return 2
	}
	if len(changes.ops) == 0 {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no changes to plan").
			WithKind(lua.Invalid).
			WithRetryable(false))
		return 2
	}
	if denied := authorizeDurableChanges(l, changes); denied != nil {
		l.Push(lua.LNil)
		l.Push(denied)
		return 2
	}
	planner, ok := changes.snapshot.reg.(regapi.Planner)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "registry cannot plan changes").
			WithKind(lua.Internal).
			WithRetryable(false))
		return 2
	}
	plan, err := planner.Plan(l.Context(), changes.snapshot.version, changes.ops)
	if err != nil {
		changes.plan = nil
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "plan changes"))
		return 2
	}
	table, convErr := planToLuaTable(l, plan)
	if convErr != nil {
		changes.plan = nil
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, convErr, "convert plan"))
		return 2
	}
	changes.plan = plan
	l.Push(table)
	l.Push(lua.LNil)
	return 2
}

func planToLuaTable(l *lua.LState, plan *regapi.Plan) (*lua.LTable, error) {
	operations := func(changes regapi.ChangeSet) (*lua.LTable, error) {
		list := l.CreateTable(len(changes), 0)
		for i, op := range changes {
			entry, err := stateEntryToLuaTable(l, op.Entry)
			if err != nil {
				return nil, err
			}
			item := l.CreateTable(0, 2)
			item.RawSetString("op", lua.LString(operationVerb(op.Kind)))
			item.RawSetString("entry", entry)
			list.RawSetInt(i+1, item)
		}
		return list, nil
	}
	changesTable, err := operations(plan.Changes)
	if err != nil {
		return nil, err
	}
	historyTable, err := operations(plan.History)
	if err != nil {
		return nil, err
	}
	effects := l.CreateTable(len(plan.Effects), 0)
	for i, target := range plan.Effects {
		item := l.CreateTable(0, 2)
		item.RawSetString("kind", lua.LString(target.Kind))
		item.RawSetString("digest", lua.LString(target.Digest))
		effects.RawSetInt(i+1, item)
	}

	result := l.CreateTable(0, 6)
	value.PushTypedUserData(l, plan.Base, typeVersion)
	result.RawSetString("base", l.Get(-1))
	l.Pop(1)
	result.RawSetString("digest", lua.LString(plan.Digest))
	result.RawSetString("changes", changesTable)
	result.RawSetString("history", historyTable)
	result.RawSetString("effects", effects)
	if plan.Resolution != nil {
		result.RawSetString("resolution", resolutionToLuaTable(l, plan.Resolution))
	}
	return result, nil
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
