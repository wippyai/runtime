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
func registerSnapshotType(l *lua.LState) {
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
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	entries, err := snap.GetAllEntries()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, err, "entries"))
		return 2
	}

	entriesTable := l.CreateTable(len(entries), 0)
	idx := 1
	for _, entry := range entries {
		if !security.IsAllowed(l.Context(), "registry.get", entry.ID.String(), nil) {
			continue
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
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	idStr := l.CheckString(2)
	id := regapi.ParseID(idStr)

	if !security.IsAllowed(l.Context(), "registry.get", id.String(), nil) {
		l.RaiseError("not allowed to access entry: %s", id.String())
		return 0
	}

	entry, err := snap.GetEntry(id)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, err, "get"))
		return 2
	}

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
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	ns := l.CheckString(2)

	var result []regapi.Entry
	for _, entry := range snap.entries {
		if entry.ID.NS == ns {
			if security.IsAllowed(l.Context(), "registry.get", entry.ID.String(), nil) {
				result = append(result, entry)
			}
		}
	}

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
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	filterTable := l.CheckTable(2)
	meta := convertFilterToMetadata(l, filterTable)

	mainFinder := regapi.GetFinder(l.Context())
	var entries []regapi.Entry
	var err error

	if mainFinder != nil {
		f := finder.Fork(mainFinder, snap, snap.log)
		entries, err = f.Find(meta)
	} else {
		f := finder.NewFinder(snap, snap.log)
		entries, err = f.Find(meta)
	}

	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, err, "find"))
		return 2
	}

	entriesTable := l.CreateTable(len(entries), 0)
	idx := 1
	for _, entry := range entries {
		if !security.IsAllowed(l.Context(), "registry.get", entry.ID.String(), nil) {
			continue
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
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	changes := &Changes{
		snapshot: snap,
		ops:      []regapi.Operation{},
		log:      snap.log,
	}

	ud := l.NewUserData()
	ud.Value = changes
	ud.Metatable = value.GetTypeMetatable(l, changesMetatable)

	l.Push(ud)
	return 1
}

// snapshotVersion returns the version of the snapshot
func snapshotVersion(l *lua.LState) int {
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	ud := wrapVersion(l, snap.version)
	l.Push(ud)
	return 1
}

// checkSnapshot checks if the first argument is a Snapshot userdata
func checkSnapshot(l *lua.LState) *Snapshot {
	ud := l.CheckUserData(1)
	if snapshot, ok := ud.Value.(*Snapshot); ok {
		return snapshot
	}
	l.ArgError(1, "snapshot expected")
	return nil
}
