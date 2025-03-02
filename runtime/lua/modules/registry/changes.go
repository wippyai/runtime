package registry

import (
	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
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
	mt := l.NewTypeMetatable(changesMetatable)
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"create": changesCreate,
		"update": changesUpdate,
		"delete": changesDelete,
		"apply":  changesApply,
	}))
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
			NS:   regapi.Namespace(ns.String()),
			Name: regapi.Name(name.String()),
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

	// Applying changes involves I/O, use coroutine
	coroutine.Wrap(l, func() *engine.Update {
		// Apply changes
		version, err := changes.snapshot.reg.Apply(l.Context(), changes.ops)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Create userdata for Version
		ud := wrapVersion(l, version)
		return engine.NewUpdate(nil, []lua.LValue{ud, lua.LNil}, nil)
	})

	return -1 // Yield for coroutine
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
