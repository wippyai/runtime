package registry

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/system/registry/topology"

	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Changes represents a set of operations to modify the registry
type Changes struct {
	snapshot *Snapshot
	ops      []regapi.Operation
	log      *zap.Logger
}

// registerChangesType registers the Changes type and methods
func registerChangesType(l *lua.LState) {
	value.RegisterMethods(l, changesMetatable, map[string]lua.LGFunction{
		"ops":    changesOps,
		"create": changesCreate,
		"update": changesUpdate,
		"delete": changesDelete,
		"apply":  changesApply,
	})
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

		entryTable, err := entryToLuaTable(l, op.Entry)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(newRegistryOperationError(l, err, "ops"))
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

	entry, err := luaTableToEntry(l, entryTable)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, err, "create"))
		return 2
	}

	changes.ops = append(changes.ops, regapi.Operation{
		Kind:  regapi.Create,
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

	entry, err := luaTableToEntry(l, entryTable)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, err, "update"))
		return 2
	}

	changes.ops = append(changes.ops, regapi.Operation{
		Kind:  regapi.Update,
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
		id = regapi.ID{
			NS:   ns.String(),
			Name: name.String(),
		}
	default:
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, fmt.Errorf("invalid ID format"), "delete"))
		return 2
	}

	changes.ops = append(changes.ops, regapi.Operation{
		Kind: regapi.Delete,
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
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, fmt.Errorf("no changes to apply"), "apply"))
		return 2
	}

	if !security.IsAllowed(l.Context(), "registry.apply", "", nil) {
		l.RaiseError("not allowed to apply registry changes")
		return 0
	}

	resolver := regapi.GetResolver(l.Context())
	stateBuilder := topology.NewStateBuilder(changes.log, resolver)
	sortedOps, err := stateBuilder.SortChangeSet(changes.snapshot.entries, changes.ops)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, fmt.Errorf("failed to sort operations: %w", err), "apply"))
		return 2
	}

	ctx := context.Background()

	version, err := changes.snapshot.reg.Apply(ctx, sortedOps)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, err, "apply"))
		return 2
	}

	ud := wrapVersion(l, version)
	l.Push(ud)
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
