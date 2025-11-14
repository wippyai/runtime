package registry

import (
	"context"
	"errors"
	"sync"

	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/runtime/lua/security"
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
	log         *zap.Logger
	moduleTable *lua.LTable
	once        sync.Once
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
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

// initModuleTable creates and initializes the module table once
func (m *Module) initModuleTable(l *lua.LState) {
	// Create module table
	t := l.CreateTable(0, 10) // Increase size to accommodate new functions

	// Register module-level functions directly
	t.RawSetString("snapshot", l.NewFunction(m.snapshotCreate))
	t.RawSetString("snapshot_at", l.NewFunction(m.snapshotAt))
	t.RawSetString("current_version", l.NewFunction(m.currentVersion))
	t.RawSetString("versions", l.NewFunction(m.versions))
	t.RawSetString("apply_version", l.NewFunction(m.applyVersion))
	t.RawSetString("parse_id", l.NewFunction(parseID))
	t.RawSetString("history", l.NewFunction(m.historyCreate))
	t.RawSetString("find", l.NewFunction(m.registryFind))
	t.RawSetString("get", l.NewFunction(m.registryGet))
	t.RawSetString("build_delta", l.NewFunction(m.buildDelta)) // Add our new function

	// Register types with their methods using the util helper functions
	m.registerSnapshotType(l)
	m.registerChangesType(l)
	m.registerVersionType(l)
	m.registerHistoryType(l)

	// Preload the loader submodule
	loaderMod := NewLoaderModule(m.log)
	l.PreloadModule(moduleName+"."+loaderModuleName, loaderMod.Loader)

	// Make the table immutable so it can be safely reused
	t.Immutable = true

	m.moduleTable = t
}

// Helper function to convert an ID table to a registry ID
func tableToID(_ *lua.LState, table *lua.LTable) (regapi.ID, error) {
	ns := table.RawGetString("ns")
	name := table.RawGetString("name")

	if ns == lua.LNil || name == lua.LNil {
		return regapi.ID{}, errors.New("id table must have ns and name fields")
	}

	return regapi.ID{
		NS:   ns.String(),
		Name: name.String(),
	}, nil
}

// parseID creates an ID from a string
func parseID(l *lua.LState) int {
	idStr := l.CheckString(1)
	id := regapi.ParseID(idStr)

	// Convert to Lua table
	idTable := l.CreateTable(0, 2)
	idTable.RawSetString("ns", lua.LString(id.NS))
	idTable.RawSetString("name", lua.LString(id.Name))

	l.Push(idTable)
	return 1
}

// Helper function to wrap a Version in a userdata
func wrapVersion(l *lua.LState, version regapi.Version) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = version
	ud.Metatable = value.GetTypeMetatable(l, versionMetatable)
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
	ud.Metatable = value.GetTypeMetatable(l, snapshotMetatable)

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
	ud := l.NewUserData()
	ud.Value = snap
	ud.Metatable = value.GetTypeMetatable(l, snapshotMetatable)

	l.Push(ud)
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
	versionsTable := l.CreateTable(len(versions), 0)
	for i, ver := range versions {
		versionsTable.RawSetInt(i+1, wrapVersion(l, ver))
	}

	l.Push(versionsTable)
	l.Push(lua.LNil)
	return 2
}

// applyVersion applies a specific version to the registry
func (m *Module) applyVersion(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "registry.apply_version", "", nil) {
		l.RaiseError("registry version change is not allowed")
		return 0
	}

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

	// We are not allowed to use thread context for registry change
	// since it is not allowed to actually cancel the operation at the moment
	ctx := context.Background()

	// Apply version
	err := reg.ApplyVersion(ctx, version)
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
	ud.Metatable = value.GetTypeMetatable(l, historyMetatable)

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

	if !security.IsAllowed(l.Context(), "registry.get", id.String(), nil) {
		l.RaiseError("registry get is not allowed for %s", id.String())
		return 0
	}

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
	finder := registry.NewFinder(reg, m.log)

	// Find entries
	entries, err := finder.Find(meta)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Convert to Lua table
	entriesTable := l.CreateTable(len(entries), 0)
	for _, entry := range entries {
		if !security.IsAllowed(l.Context(), "registry.get", entry.ID.String(), nil) {
			continue
		}

		entryTable, err := entryToLuaTable(l, entry)

		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
		entriesTable.Append(entryTable)
	}

	l.Push(entriesTable)
	l.Push(lua.LNil)
	return 2
}
