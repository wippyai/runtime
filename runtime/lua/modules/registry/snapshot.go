package registry

import (
	"fmt"

	lua "github.com/wippyai/go-lua"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/security"
	"github.com/wippyai/runtime/system/registry/finder"
	"go.uber.org/zap"
)

// Snapshot represents a point-in-time view of the registry
type Snapshot struct {
	reg     regapi.Registry
	version regapi.Version
	log     *zap.Logger
	entries []regapi.Entry
}

// GetAllEntries returns all entries in the snapshot
func (s *Snapshot) GetAllEntries() ([]regapi.Entry, error) {
	return s.entries, nil
}

// GetEntry returns a specific entry by ID
func (s *Snapshot) GetEntry(id regapi.ID) (regapi.Entry, error) {
	for _, entry := range s.entries {
		if entry.ID.Equal(id) {
			return entry, nil
		}
	}
	return regapi.Entry{}, fmt.Errorf("entry not found: %s", id)
}

// snapshotEntries returns all entries in the snapshot
func snapshotEntries(l *lua.LState) int {
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	entries, getErr := snap.GetAllEntries()
	if getErr != nil {
		err := lua.WrapErrorWithLua(l, getErr, "get entries").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	entriesTable := l.CreateTable(len(entries), 0)
	idx := 1
	for _, entry := range entries {
		if !security.IsAllowed(l.Context(), "registry.get", entry.ID.String(), nil) {
			continue
		}

		entryTable, convErr := entryToLuaTable(l, entry)
		if convErr != nil {
			err := lua.WrapErrorWithLua(l, convErr, "convert entry").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}
		entriesTable.RawSetInt(idx, entryTable)
		idx++
	}

	l.Push(entriesTable)
	l.Push(lua.LNil)
	return 2
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
		err := lua.NewLuaError(l, "not allowed to access entry: "+id.String()).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	entry, getErr := snap.GetEntry(id)
	if getErr != nil {
		err := lua.NewLuaError(l, "entry not found: "+id.String()).
			WithKind(lua.NotFound).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	entryTable, convErr := entryToLuaTable(l, entry)
	if convErr != nil {
		err := lua.WrapErrorWithLua(l, convErr, "convert entry").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
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

	entriesTable := l.CreateTable(len(result), 0)
	for i, entry := range result {
		entryTable, convErr := entryToLuaTable(l, entry)
		if convErr != nil {
			err := lua.WrapErrorWithLua(l, convErr, "convert entry").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
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
	var findErr error

	if mainFinder != nil {
		f := finder.Fork(mainFinder, snap, snap.log)
		entries, findErr = f.Find(meta)
	} else {
		f := finder.NewFinder(snap, snap.log)
		entries, findErr = f.Find(meta)
	}

	if findErr != nil {
		err := lua.WrapErrorWithLua(l, findErr, "find entries").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	entriesTable := l.CreateTable(len(entries), 0)
	idx := 1
	for _, entry := range entries {
		if !security.IsAllowed(l.Context(), "registry.get", entry.ID.String(), nil) {
			continue
		}

		entryTable, convErr := entryToLuaTable(l, entry)
		if convErr != nil {
			err := lua.WrapErrorWithLua(l, convErr, "convert entry").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
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

	value.PushTypedUserData(l, changes, typeChanges)
	return 1
}

// snapshotVersion returns the version of the snapshot
func snapshotVersion(l *lua.LState) int {
	snap := checkSnapshot(l)
	if snap == nil {
		return 0
	}

	value.PushTypedUserData(l, snap.version, typeVersion)
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
