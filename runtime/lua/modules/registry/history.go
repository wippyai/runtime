package registry

import (
	"fmt"

	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/system/registry/topology"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// History represents access to the registry version history
type History struct {
	reg  regapi.Registry
	hist regapi.History
	log  *zap.Logger
}

// registerHistoryType registers the History type and methods
func (m *Module) registerHistoryType(l *lua.LState) {
	value.RegisterMethods(l, historyMetatable, map[string]lua.LGFunction{
		"versions":    historyVersions,
		"get_version": historyGetVersion,
		"snapshot_at": historySnapshotAt,
	})
}

// historyVersions returns all available versions in the registry history
func historyVersions(l *lua.LState) int {
	// Get history
	history := checkHistory(l)
	if history == nil {
		return 0
	}

	// Get versions from history
	versions, err := history.hist.Versions()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, err, "versions"))
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

// historyGetVersion retrieves a specific version by ID
func historyGetVersion(l *lua.LState) int {
	// Get history
	history := checkHistory(l)
	if history == nil {
		return 0
	}

	// Get version ID - parameter check
	vID := l.CheckNumber(2)
	if vID <= 0 {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, fmt.Errorf("invalid version ID"), "get_version"))
		return 2
	}

	// Get versions from history
	versions, err := history.hist.Versions()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, err, "get_version"))
		return 2
	}

	// Find the requested version
	var foundVersion regapi.Version
	for _, ver := range versions {
		if ver.ID() == uint(vID) {
			foundVersion = ver
			break
		}
	}

	if foundVersion == nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, fmt.Errorf("version not found"), "get_version"))
		return 2
	}

	// Create userdata for Version
	ud := wrapVersion(l, foundVersion)
	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// historySnapshotAt returns a snapshot of the registry at a specific version
func historySnapshotAt(l *lua.LState) int {
	// Get history
	history := checkHistory(l)
	if history == nil {
		return 0
	}

	// Get version - parameter check
	ud := l.CheckUserData(2)
	version, ok := ud.Value.(regapi.Version)
	if !ok {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, fmt.Errorf("expected version object"), "snapshot_at"))
		return 2
	}

	// Create state builder
	resolver := regapi.GetResolver(l.Context())
	stateBuilder := topology.NewStateBuilder(history.log, resolver)

	// Build state at the specified version
	state, err := stateBuilder.BuildState(history.hist, version)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newRegistryOperationError(l, err, "snapshot_at"))
		return 2
	}

	// Create snapshot from state
	snap := &Snapshot{
		reg:     history.reg,
		version: version,
		entries: state,
		log:     history.log,
	}

	// Create userdata
	snapUD := l.NewUserData()
	snapUD.Value = snap
	l.SetMetatable(snapUD, l.GetTypeMetatable(snapshotMetatable))

	l.Push(snapUD)
	l.Push(lua.LNil)
	return 2
}

// Helper function to check if the first argument is a History and return it
func checkHistory(l *lua.LState) *History {
	ud := l.CheckUserData(1)
	if history, ok := ud.Value.(*History); ok {
		return history
	}
	l.ArgError(1, "history expected")
	return nil
}
