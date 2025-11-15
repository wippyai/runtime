package registry

import (
	"fmt"

	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	"github.com/wippyai/runtime/system/registry/finder"
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

// GetAllEntries returns all entries in the snapshot
func (s *Snapshot) GetAllEntries() ([]regapi.Entry, error) {
	return s.entries, nil
}

// GetEntry returns a specific entry by ID
func (s *Snapshot) GetEntry(id regapi.ID) (regapi.Entry, error) {
	for _, entry := range s.entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return regapi.Entry{}, fmt.Errorf("entry not found: %s", id)
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
	snap := CheckSnapshot(l)
	if snap == nil {
		return 0
	}

	// Get all entries using the EntryReader interface method
	entries, err := snap.GetAllEntries()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, err, "entries"))
		return 2
	}

	// Convert to Lua table with security filtering
	entriesTable := l.CreateTable(len(entries), 0)
	idx := 1
	for _, entry := range entries {
		// Add security check for each entry
		if !security.IsAllowed(l.Context(), "registry.get", entry.ID.String(), nil) {
			continue // Skip entries the user doesn't have permission to access
		}

		entryTable, err := entryToLuaTable(l, entry)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(newRegistryOperationError(l, err, "entries"))
			return 2
		}
		entriesTable.RawSetInt(idx, entryTable)
		idx++
	}

	l.Push(entriesTable)
	return 1
}

// snapshotGet retrieves a specific entry by ID
func snapshotGet(l *lua.LState) int {
	// Get snapshot
	snap := CheckSnapshot(l)
	if snap == nil {
		return 0
	}

	// Get ID
	idStr := l.CheckString(2)
	id := regapi.ParseID(idStr)

	// Add security check for getting a specific entry
	if !security.IsAllowed(l.Context(), "registry.get", id.String(), nil) {
		l.RaiseError("not allowed to access entry: %s", id.String())
		return 0
	}

	// Find entry using the EntryReader interface method
	entry, err := snap.GetEntry(id)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, err, "get"))
		return 2
	}

	// Convert to Lua table
	entryTable, err := entryToLuaTable(l, entry)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, err, "get"))
		return 2
	}

	l.Push(entryTable)
	l.Push(lua.LNil)
	return 2
}

// snapshotNamespace returns all entries in a namespace
func snapshotNamespace(l *lua.LState) int {
	// Get snapshot
	snap := CheckSnapshot(l)
	if snap == nil {
		return 0
	}

	// Get namespace
	ns := l.CheckString(2)

	// Filter entries by namespace - this is a simple operation with in-memory data
	var result []regapi.Entry
	for _, entry := range snap.entries {
		if entry.ID.NS == ns {
			// Add security check for each entry
			if security.IsAllowed(l.Context(), "registry.get", entry.ID.String(), nil) {
				result = append(result, entry)
			}
		}
	}

	// Convert to Lua table
	entriesTable := l.NewTable()
	for i, entry := range result {
		entryTable, err := entryToLuaTable(l, entry)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(newRegistryOperationError(l, err, "namespace"))
			return 2
		}
		entriesTable.RawSetInt(i+1, entryTable)
	}

	l.Push(entriesTable)
	return 1
}

// snapshotFind returns entries matching criteria using the Finder interface
func snapshotFind(l *lua.LState) int {
	// Get snapshot
	snap := CheckSnapshot(l)
	if snap == nil {
		return 0
	}

	// Get filter criteria
	filterTable := l.CheckTable(2)

	// Convert filter to metadata for finder
	meta := convertFilterToMetadata(l, filterTable)

	// Get main finder from context and fork it for this snapshot
	// This shares the regex cache across all finders (main + snapshots)
	mainFinder := regapi.GetFinder(l.Context())
	var entries []regapi.Entry
	var err error

	if mainFinder != nil {
		// Fork from main finder to share regex cache
		f := finder.Fork(mainFinder, snap, snap.log)
		entries, err = f.Find(meta)
	} else {
		// Fallback: create new finder if main finder not in context
		f := finder.NewFinder(snap, snap.log)
		entries, err = f.Find(meta)
	}

	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, err, "find"))
		return 2
	}

	// Convert to Lua table with security filtering
	entriesTable := l.CreateTable(len(entries), 0)
	idx := 1
	for _, entry := range entries {
		// Add security check for each entry
		if !security.IsAllowed(l.Context(), "registry.get", entry.ID.String(), nil) {
			continue // Skip entries the user doesn't have permission to access
		}

		entryTable, err := entryToLuaTable(l, entry)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(newRegistryOperationError(l, err, "find"))
			return 2
		}
		entriesTable.RawSetInt(idx, entryTable)
		idx++
	}

	l.Push(entriesTable)
	return 1
}

// snapshotChanges creates a changeset for modifying the registry
func snapshotChanges(l *lua.LState) int {
	// Get snapshot
	snap := CheckSnapshot(l)
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
	ud.Metatable = value.GetTypeMetatable(l, changesMetatable)

	l.Push(ud)
	return 1
}

// snapshotVersion returns the version of the snapshot
func snapshotVersion(l *lua.LState) int {
	// Get snapshot
	snap := CheckSnapshot(l)
	if snap == nil {
		return 0
	}

	// Create userdata for Version
	ud := wrapVersion(l, snap.version)
	l.Push(ud)
	return 1
}

// CheckSnapshot checks if the first argument is a Snapshot userdata
func CheckSnapshot(l *lua.LState) *Snapshot {
	ud := l.CheckUserData(1)
	if snapshot, ok := ud.Value.(*Snapshot); ok {
		return snapshot
	}
	l.ArgError(1, "snapshot expected")
	return nil
}
