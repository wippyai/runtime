package registry

import (
	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/system/registry/topology"
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

	// Getting versions involves I/O, use coroutine
	coroutine.Wrap(l, func() *engine.Update {
		// Get versions from history
		versions, err := history.hist.Versions()
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Convert to Lua table
		versionsTable := l.NewTable()
		for i, ver := range versions {
			// Create userdata for Version
			ud := wrapVersion(l, ver)
			versionsTable.RawSetInt(i+1, ud)
		}

		return engine.NewUpdate(nil, []lua.LValue{versionsTable, lua.LNil}, nil)
	})

	return -1 // Yield for coroutine
}

// historyGetVersion retrieves a specific version by ID
func historyGetVersion(l *lua.LState) int {
	// Get history
	history := checkHistory(l)
	if history == nil {
		return 0
	}

	// Get version ID - parameter check, no coroutine needed
	vID := l.CheckNumber(2)
	if vID <= 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid version ID"))
		return 2
	}

	// Getting versions involves I/O, use coroutine
	coroutine.Wrap(l, func() *engine.Update {
		// Get versions from history
		versions, err := history.hist.Versions()
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
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
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString("version not found")}, nil)
		}

		// Create userdata for Version
		ud := wrapVersion(l, foundVersion)
		return engine.NewUpdate(nil, []lua.LValue{ud, lua.LNil}, nil)
	})

	return -1 // Yield for coroutine
}

// historySnapshotAt returns a snapshot of the registry at a specific version
func historySnapshotAt(l *lua.LState) int {
	// Get history
	history := checkHistory(l)
	if history == nil {
		return 0
	}

	// Get version - parameter check, no coroutine needed
	ud := l.CheckUserData(2)
	version, ok := ud.Value.(regapi.Version)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("expected version object"))
		return 2
	}

	// Building state at a specified version involves I/O, use coroutine
	coroutine.Wrap(l, func() *engine.Update {
		// Create state builder
		stateBuilder := topology.NewStateBuilder(history.log)

		// Build state at the specified version
		state, err := stateBuilder.BuildState(history.hist, version)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
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

		return engine.NewUpdate(nil, []lua.LValue{snapUD, lua.LNil}, nil)
	})

	return -1 // Yield for coroutine
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
