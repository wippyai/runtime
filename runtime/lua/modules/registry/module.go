package registry

import (
	"errors"
	"fmt"

	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/registry"
	"github.com/ponyruntime/pony/system/registry/topology"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const (
	// Module name
	moduleName = "registry"

	// Metatables
	snapshotMetatable = "registry.Snapshot"
	changesMetatable  = "registry.Changes"
	versionMetatable  = "registry.Version"
	historyMetatable  = "registry.History"
)

// Module represents the registry module
type Module struct {
	log *zap.Logger
}

// NewRegistryModule creates a new registry module
func NewRegistryModule(log *zap.Logger) *Module {
	if log == nil {
		log = zap.NewNop()
	}
	return &Module{log: log}
}

// Name returns the module name
func (m *Module) Name() string {
	return moduleName
}

// Loader loads the module into the Lua state
func (m *Module) Loader(l *lua.LState) int {
	// Create module table
	mod := l.CreateTable(0, 10) // Increase size to accommodate new functions

	// Register module-level functions directly
	mod.RawSetString("snapshot", l.NewFunction(m.snapshotCreate))
	mod.RawSetString("snapshot_at", l.NewFunction(m.snapshotAt))
	mod.RawSetString("current_version", l.NewFunction(m.currentVersion))
	mod.RawSetString("versions", l.NewFunction(m.versions))
	mod.RawSetString("apply_version", l.NewFunction(m.applyVersion))
	mod.RawSetString("parse_id", l.NewFunction(parseID))
	mod.RawSetString("history", l.NewFunction(m.historyCreate))
	mod.RawSetString("find", l.NewFunction(m.registryFind))
	mod.RawSetString("get", l.NewFunction(m.registryGet))
	mod.RawSetString("build_delta", l.NewFunction(m.buildDelta)) // Add our new function

	// Register types with their methods using the util helper functions
	m.registerSnapshotType(l)
	m.registerChangesType(l)
	m.registerVersionType(l)
	m.registerHistoryType(l)

	// Preload the loader submodule
	loaderMod := NewLoaderModule(m.log)
	l.PreloadModule(moduleName+"."+loaderModuleName, loaderMod.Loader)

	// Push the module
	l.Push(mod)
	return 1
}

// Helper function to convert an ID table to a registry ID
func tableToID(l *lua.LState, table *lua.LTable) (regapi.ID, error) {
	ns := table.RawGetString("ns")
	name := table.RawGetString("name")

	if ns == lua.LNil || name == lua.LNil {
		return regapi.ID{}, errors.New("id table must have ns and name fields")
	}

	return regapi.ID{
		NS:   regapi.Namespace(ns.String()),
		Name: regapi.Name(name.String()),
	}, nil
}

// parseID creates an ID from a string
func parseID(l *lua.LState) int {
	idStr := l.CheckString(1)
	id := regapi.ParseID(idStr)

	// Convert to Lua table
	idTable := l.NewTable()
	idTable.RawSetString("ns", lua.LString(id.NS))
	idTable.RawSetString("name", lua.LString(id.Name))

	l.Push(idTable)
	return 1
}

// Helper function to wrap a Version in a userdata
func wrapVersion(l *lua.LState, version regapi.Version) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = version
	l.SetMetatable(ud, l.GetTypeMetatable(versionMetatable))
	return ud
}

// snapshotCreate returns a new snapshot of the registry at the current version
func (m *Module) snapshotCreate(l *lua.LState) int {
	// Get registry from context
	reg := regapi.GetRegistry(l.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("registry not found in context"))
		return 2
	}

	// Get current version
	version, err := reg.Current()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Get all entries
	entries, err := reg.GetAllEntries()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create snapshot
	snap := &Snapshot{
		reg:     reg,
		version: version,
		entries: entries,
		log:     m.log,
	}

	// Create userdata
	ud := l.NewUserData()
	ud.Value = snap
	l.SetMetatable(ud, l.GetTypeMetatable(snapshotMetatable))

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// snapshotAt returns a snapshot of the registry at the specified version
func (m *Module) snapshotAt(l *lua.LState) int {
	// Get registry from context
	reg := regapi.GetRegistry(l.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("registry not found in context"))
		return 2
	}

	// Get version ID - this is a simple parameter check, no coroutine needed
	versionID := l.CheckNumber(1)
	if versionID <= 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid version ID"))
		return 2
	}

	// Get history from registry
	history := reg.History()
	if history == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("history not available"))
		return 2
	}

	// Get all versions from history
	versions, err := history.Versions()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Find the requested version
	var foundVersion regapi.Version
	for _, ver := range versions {
		if ver.ID() == uint(versionID) {
			foundVersion = ver
			break
		}
	}

	if foundVersion == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("version not found"))
		return 2
	}

	// Create state builder
	stateBuilder := topology.NewStateBuilder(m.log)

	// Build state at the specified version
	state, err := stateBuilder.BuildState(history, foundVersion)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create snapshot from state
	snap := &Snapshot{
		reg:     reg,
		version: foundVersion,
		entries: state,
		log:     m.log,
	}

	// Create userdata
	snapUD := l.NewUserData()
	snapUD.Value = snap
	l.SetMetatable(snapUD, l.GetTypeMetatable(snapshotMetatable))

	l.Push(snapUD)
	l.Push(lua.LNil)
	return 2
}

// currentVersion returns the current version of the registry
func (m *Module) currentVersion(l *lua.LState) int {
	// Get registry from context
	reg := regapi.GetRegistry(l.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("registry not found in context"))
		return 2
	}

	// Get current version
	version, err := reg.Current()
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

// versions returns all available versions in the registry history
func (m *Module) versions(l *lua.LState) int {
	// Get registry from context
	reg := regapi.GetRegistry(l.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("registry not found in context"))
		return 2
	}

	// Get history from registry
	history := reg.History()
	if history == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("history not available"))
		return 2
	}

	// Get versions from history
	versions, err := history.Versions()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Convert to Lua table
	versionsTable := l.NewTable()
	for i, ver := range versions {
		// Create userdata for Version
		ud := wrapVersion(l, ver)
		versionsTable.RawSetInt(i+1, ud)
	}

	l.Push(versionsTable)
	l.Push(lua.LNil)
	return 2
}

// applyVersion applies a specific version to the registry
func (m *Module) applyVersion(l *lua.LState) int {
	// Get registry from context
	reg := regapi.GetRegistry(l.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("registry not found in context"))
		return 2
	}

	// Get version - parameter check, no coroutine needed
	ud := l.CheckUserData(1)
	version, ok := ud.Value.(regapi.Version)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("expected version object"))
		return 2
	}

	// Apply version
	err := reg.ApplyVersion(l.Context(), version)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// historyCreate returns the history interface for the registry
func (m *Module) historyCreate(l *lua.LState) int {
	// Get registry from context - simple context check, no coroutine needed
	reg := regapi.GetRegistry(l.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("registry not found in context"))
		return 2
	}

	// Get history from registry - simple operation, no coroutine needed
	history := reg.History()
	if history == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("history not available"))
		return 2
	}

	// Create history wrapper
	hist := &History{
		reg:  reg,
		hist: history,
		log:  m.log,
	}

	// Create userdata
	ud := l.NewUserData()
	ud.Value = hist
	l.SetMetatable(ud, l.GetTypeMetatable(historyMetatable))

	l.Push(ud)
	return 1
}

// registryGet retrieves a specific entry by ID from the registry
func (m *Module) registryGet(l *lua.LState) int {
	// Get registry from context
	reg := regapi.GetRegistry(l.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("registry not found in context"))
		return 2
	}

	// Get ID
	idStr := l.CheckString(1)
	id := regapi.ParseID(idStr)

	// Get entry
	entry, err := reg.GetEntry(id)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Convert to Lua table
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

// registryFind implements registry-level search using the Finder interface
func (m *Module) registryFind(l *lua.LState) int {
	// Get registry from context
	reg := regapi.GetRegistry(l.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("registry not found in context"))
		return 2
	}

	// Get filter criteria
	filterTable := l.CheckTable(1)
	if filterTable == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("filter criteria table required"))
		return 2
	}

	// Convert Lua table to registry metadata
	meta := convertFilterToMetadata(l, filterTable)

	// Create finder
	finder := registry.NewFinder(reg)

	// Find entries
	entries, err := finder.Find(meta)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Convert to Lua table
	entriesTable := l.NewTable()
	for i, entry := range entries {
		entryTable, err := entryToLuaTable(l, entry)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
		entriesTable.RawSetInt(i+1, entryTable)
	}

	l.Push(entriesTable)
	l.Push(lua.LNil)
	return 2
}
