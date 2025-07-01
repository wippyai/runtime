package registry

import (
	"context"
	"github.com/ponyruntime/pony/system/registry/topology"

	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/runtime/lua/security"
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
func (m *Module) registerChangesType(l *lua.LState) {
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
	// Get changes
	changes := checkChanges(l)
	if changes == nil {
		return 0
	}

	// Create table for ops
	opsTable := l.CreateTable(len(changes.ops), 0)

	// Add each operation to the table
	for i, op := range changes.ops {
		opTable := l.CreateTable(0, 2)
		opTable.RawSetString("kind", lua.LString(op.Kind))

		// Convert entry to table
		entryTable, err := entryToLuaTable(l, op.Entry)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}

		opTable.RawSetString("entry", entryTable)

		// Add to ops table
		opsTable.RawSetInt(i+1, opTable)
	}

	l.Push(opsTable)
	return 1
}

// changesCreate adds a new entry to the changeset
func changesCreate(l *lua.LState) int {
	// Get changes
	changes := checkChanges(l)
	if changes == nil {
		return 0
	}

	// Get entry table - parameter check, no coroutine needed
	entryTable := l.CheckTable(2)

	// Convert to entry
	entry, err := luaTableToEntry(l, entryTable)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Add to changes - this is a simple in-memory operation
	changes.ops = append(changes.ops, regapi.Operation{
		Kind:  regapi.Create,
		Entry: entry,
	})

	// Return changes for chaining
	l.Push(l.Get(1))
	return 1
}

// changesUpdate adds an entry update to the changeset
func changesUpdate(l *lua.LState) int {
	// Get changes
	changes := checkChanges(l)
	if changes == nil {
		return 0
	}

	// Get entry table - parameter check, no coroutine needed
	entryTable := l.CheckTable(2)

	// Convert to entry
	entry, err := luaTableToEntry(l, entryTable)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Add to changes - this is a simple in-memory operation
	changes.ops = append(changes.ops, regapi.Operation{
		Kind:  regapi.Update,
		Entry: entry,
	})

	// Return changes for chaining
	l.Push(l.Get(1))
	return 1
}

// changesDelete adds an entry deletion to the changeset
func changesDelete(l *lua.LState) int {
	// Get changes
	changes := checkChanges(l)
	if changes == nil {
		return 0
	}

	// Get ID - parameter check, no coroutine needed
	idVal := l.Get(2)
	var id regapi.ID

	switch v := idVal.(type) {
	case lua.LString:
		// Parse string ID
		id = regapi.ParseID(string(v))
	case *lua.LTable:
		// Parse table ID
		ns := v.RawGetString("ns")
		name := v.RawGetString("name")
		id = regapi.ID{
			NS:   ns.String(),
			Name: name.String(),
		}
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid ID format"))
		return 2
	}

	// Add to changes - this is a simple in-memory operation
	changes.ops = append(changes.ops, regapi.Operation{
		Kind: regapi.Delete,
		Entry: regapi.Entry{
			ID: id,
		},
	})

	// Return changes for chaining
	l.Push(l.Get(1))
	return 1
}

// changesApply applies the changeset to create a new version
func changesApply(l *lua.LState) int {
	// Get changes
	changes := checkChanges(l)
	if changes == nil {
		return 0
	}

	// Check if there are any changes - simple check, no coroutine needed
	if len(changes.ops) == 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("no changes to apply"))
		return 2
	}

	// Add security check for applying changes
	if !security.IsAllowed(l.Context(), "registry.apply", "", nil) {
		l.RaiseError("not allowed to apply registry changes")
		return 0
	}

	// Sort operations by dependencies before applying
	stateBuilder := topology.NewStateBuilder(changes.log)
	sortedOps, err := stateBuilder.SortChangeSet(changes.snapshot.entries, changes.ops)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to sort operations: " + err.Error()))
		return 2
	}

	// We are not allowed to use thread context for registry change
	// since it is not allowed to actually cancel the operation at the moment
	ctx := context.Background() // todo: the proper fix will require rollback improvement in registry

	// Apply sorted changes
	version, err := changes.snapshot.reg.Apply(ctx, sortedOps)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create userdata for Version
	ud := wrapVersion(l, version)
	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// Helper function to check if the first argument is a Changes and return it
func checkChanges(l *lua.LState) *Changes {
	ud := l.CheckUserData(1)
	if changes, ok := ud.Value.(*Changes); ok {
		return changes
	}
	l.ArgError(1, "changes expected")
	return nil
}
