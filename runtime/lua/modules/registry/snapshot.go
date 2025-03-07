package registry

import (
	"fmt"
	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Snapshot represents a point-in-time view of the registry
type Snapshot struct {
	reg     regapi.Registry
	version regapi.Version
	entries []regapi.Entry
	log     *zap.Logger
}

// registerSnapshotType registers the Snapshot type and methods
func (m *Module) registerSnapshotType(l *lua.LState) {
	value.RegisterMethods(l, snapshotMetatable, map[string]lua.LGFunction{
		"entries":   snapshotEntries,
		"get":       snapshotGet,
		"namespace": snapshotNamespace,
		"find":      snapshotFind,
		"changes":   snapshotChanges,
		"version":   snapshotVersion,
	})
}

// snapshotEntries returns all entries in the snapshot
func snapshotEntries(l *lua.LState) int {
	// Get snapshot
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	// Convert to Lua table - this is a simple operation with in-memory data
	entriesTable := l.NewTable()
	for i, entry := range snap.entries {
		entryTable, err := entryToLuaTable(l, entry)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
		entriesTable.RawSetInt(i+1, entryTable)
	}

	l.Push(entriesTable)
	return 1
}

// snapshotGet retrieves a specific entry by ID
func snapshotGet(l *lua.LState) int {
	// Get snapshot
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	// Get ID
	idStr := l.CheckString(2)
	id := regapi.ParseID(idStr)

	// Find entry - this is a simple operation with in-memory data
	for _, entry := range snap.entries {
		if entry.ID.NS == id.NS && entry.ID.Name == id.Name {
			entryTable, err := entryToLuaTable(l, entry)
			if err != nil {
				l.Push(lua.LNil)
				l.Push(lua.LString(err.Error()))
				return 2
			}

			l.Push(entryTable)
			l.Push(lua.LNil)
			return 2
		}
	}

	l.Push(lua.LNil)
	l.Push(lua.LString(fmt.Sprintf("entry not found: %s", id)))
	return 2
}

// snapshotNamespace returns all entries in a namespace
func snapshotNamespace(l *lua.LState) int {
	// Get snapshot
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	// Get namespace
	ns := l.CheckString(2)

	// Filter entries by namespace - this is a simple operation with in-memory data
	var result []regapi.Entry
	for _, entry := range snap.entries {
		if entry.ID.NS == regapi.Namespace(ns) {
			result = append(result, entry)
		}
	}

	// Convert to Lua table
	entriesTable := l.NewTable()
	for i, entry := range result {
		entryTable, err := entryToLuaTable(l, entry)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
		entriesTable.RawSetInt(i+1, entryTable)
	}

	l.Push(entriesTable)
	return 1
}

// snapshotFind returns entries matching criteria
func snapshotFind(l *lua.LState) int {
	// Get snapshot
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	// Get filter criteria
	filterTable := l.CheckTable(2)

	// Convert filter to metadata for finder
	meta := convertFilterToMetadata(l, filterTable)

	// Create in-memory finder for snapshot entries
	// (We're using snapshot-local search instead of registry-wide search)
	var result []regapi.Entry

	// Create metamatcher from metadata
	matcher := metadataToMatcher(meta) // todo: REDO TO USE FINDER!!!

	// Filter entries using the matcher - this is a simple operation with in-memory data
	for _, entry := range snap.entries {
		// Create augmented metadata with ID fields for matching
		augMeta := make(regapi.Metadata)

		// Copy original metadata
		for k, v := range entry.Meta {
			augMeta[k] = v
		}

		if matcher.Match(augMeta) {
			result = append(result, entry)
		}
	}

	// Convert to Lua table
	entriesTable := l.NewTable()
	for i, entry := range result {
		entryTable, err := entryToLuaTable(l, entry)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
		entriesTable.RawSetInt(i+1, entryTable)
	}

	l.Push(entriesTable)
	return 1
}

// snapshotChanges creates a changeset for modifying the registry
func snapshotChanges(l *lua.LState) int {
	// Get snapshot
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	// Create changes
	changes := &Changes{
		snapshot: snap,
		ops:      []regapi.Operation{},
		log:      snap.log,
	}

	// Create userdata
	ud := l.NewUserData()
	ud.Value = changes
	l.SetMetatable(ud, l.GetTypeMetatable(changesMetatable))

	l.Push(ud)
	return 1
}

// snapshotVersion returns the version of the snapshot
func snapshotVersion(l *lua.LState) int {
	// Get snapshot
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	// Create userdata for Version
	ud := wrapVersion(l, snap.version)
	l.Push(ud)
	return 1
}

// Helper function to check if the first argument is a Snapshot and return it
func checkSnapshot(l *lua.LState) *Snapshot {
	ud := l.CheckUserData(1)
	if snapshot, ok := ud.Value.(*Snapshot); ok {
		return snapshot
	}
	l.ArgError(1, "snapshot expected")
	return nil
}
