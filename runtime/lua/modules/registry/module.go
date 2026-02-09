package registry

import (
	"errors"
	"strconv"

	lua "github.com/wippyai/go-lua"
	regapi "github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/security"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

const (
	typeSnapshot = "registry.Snapshot"
	typeChanges  = "registry.Changes"
	typeVersion  = "registry.Version"
	typeHistory  = "registry.History"
)

func init() {
	value.RegisterTypeMethods(nil, typeSnapshot,
		map[string]lua.LGoFunc{"__tostring": snapshotToString},
		map[string]lua.LGoFunc{
			"entries":   snapshotEntries,
			"get":       snapshotGet,
			"namespace": snapshotNamespace,
			"find":      snapshotFind,
			"changes":   snapshotChanges,
			"version":   snapshotVersion,
		})

	value.RegisterTypeMethods(nil, typeChanges,
		map[string]lua.LGoFunc{"__tostring": changesToString},
		map[string]lua.LGoFunc{
			"ops":    changesOps,
			"create": changesCreate,
			"update": changesUpdate,
			"delete": changesDelete,
			"apply":  changesApply,
		})

	value.RegisterTypeMethods(nil, typeVersion,
		map[string]lua.LGoFunc{"__tostring": versionToString},
		map[string]lua.LGoFunc{
			"id":       versionID,
			"previous": versionPrevious,
			"next":     versionNext,
			"string":   versionString,
		})

	value.RegisterTypeMethods(nil, typeHistory,
		map[string]lua.LGoFunc{"__tostring": historyToString},
		map[string]lua.LGoFunc{
			"versions":    historyVersions,
			"get_version": historyGetVersion,
			"snapshot_at": historySnapshotAt,
		})
}

// Options for registry module configuration.
type Options struct {
	Log *zap.Logger
}

// DefaultOptions returns default configuration.
func DefaultOptions() Options {
	return Options{
		Log: zap.NewNop(),
	}
}

// Module is the default registry module with default options.
var Module = NewModule(DefaultOptions())

// NewModule creates a registry module with given options.
func NewModule(opts Options) *luaapi.ModuleDef {
	if opts.Log == nil {
		opts.Log = zap.NewNop()
	}

	return &luaapi.ModuleDef{
		Name:        "registry",
		Description: "Registry operations for entries, snapshots, and versioning",
		Class:       []string{luaapi.ClassNondeterministic, luaapi.ClassStorage},
		Build: func() (*lua.LTable, []luaapi.YieldType) {
			mod := lua.CreateTable(0, 10)
			mod.RawSetString("get", lua.LGoFunc(registryGet))
			mod.RawSetString("find", lua.LGoFunc(registryFind))
			mod.RawSetString("parse_id", lua.LGoFunc(parseID))
			mod.RawSetString("snapshot", lua.LGoFunc(registrySnapshot))
			mod.RawSetString("snapshot_at", makeSnapshotAt(opts.Log))
			mod.RawSetString("current_version", lua.LGoFunc(registryCurrentVersion))
			mod.RawSetString("versions", lua.LGoFunc(registryVersions))
			mod.RawSetString("history", lua.LGoFunc(registryHistory))
			mod.RawSetString("apply_version", lua.LGoFunc(registryApplyVersion))
			mod.RawSetString("build_delta", makeBuildDelta(opts.Log))
			mod.Immutable = true
			return mod, nil
		},
		Types: ModuleTypes,
	}
}

// Module functions

func parseID(l *lua.LState) int {
	idStr := l.CheckString(1)
	id := regapi.ParseID(idStr)

	idTable := l.CreateTable(0, 2)
	idTable.RawSetString("ns", lua.LString(id.NS))
	idTable.RawSetString("name", lua.LString(id.Name))

	l.Push(idTable)
	return 1
}

func registryGet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		err := lua.NewLuaError(l, "registry not found in context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	idStr := l.CheckString(1)
	id := regapi.ParseID(idStr)

	if !security.IsAllowed(ctx, "registry.get", id.String(), nil) {
		err := lua.NewLuaError(l, "not allowed to access entry: "+id.String()).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	entry, getErr := reg.GetEntry(id)
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

func registryFind(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		err := lua.NewLuaError(l, "registry not found in context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	filterTable := l.CheckTable(1)
	if filterTable == nil {
		err := lua.NewLuaError(l, "filter criteria table required").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	meta := convertFilterToMetadata(l, filterTable)

	finder := regapi.GetFinder(ctx)
	if finder == nil {
		err := lua.NewLuaError(l, "finder not available in context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	entries, findErr := finder.Find(meta)
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
		if !security.IsAllowed(ctx, "registry.get", entry.ID.String(), nil) {
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

func registrySnapshot(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		err := lua.NewLuaError(l, "registry not found in context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	version, verErr := reg.Current()
	if verErr != nil {
		err := lua.WrapErrorWithLua(l, verErr, "get current version").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	// Get entries directly from registry state (includes baseline + all applied changes)
	entries, entriesErr := reg.GetAllEntries()
	if entriesErr != nil {
		err := lua.WrapErrorWithLua(l, entriesErr, "get registry entries").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	snap := &Snapshot{
		reg:     reg,
		version: version,
		entries: entries,
		log:     zap.NewNop(),
	}

	value.PushTypedUserData(l, snap, typeSnapshot)
	l.Push(lua.LNil)
	return 2
}

func registryCurrentVersion(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		err := lua.NewLuaError(l, "registry not found in context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	version, verErr := reg.Current()
	if verErr != nil {
		err := lua.WrapErrorWithLua(l, verErr, "get current version").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	value.PushTypedUserData(l, version, typeVersion)
	l.Push(lua.LNil)
	return 2
}

func registryVersions(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		err := lua.NewLuaError(l, "registry not found in context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	hist := reg.History()
	if hist == nil {
		err := lua.NewLuaError(l, "history not available").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	versions, versErr := hist.Versions()
	if versErr != nil {
		err := lua.WrapErrorWithLua(l, versErr, "get versions").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	versionsTable := l.CreateTable(len(versions), 0)
	for i, ver := range versions {
		value.PushTypedUserData(l, ver, typeVersion)
		versionsTable.RawSetInt(i+1, l.Get(-1))
		l.Pop(1)
	}

	l.Push(versionsTable)
	l.Push(lua.LNil)
	return 2
}

func registryHistory(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		err := lua.NewLuaError(l, "registry not found in context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	hist := reg.History()
	if hist == nil {
		err := lua.NewLuaError(l, "history not available").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	history := &History{
		reg:  reg,
		hist: hist,
		log:  zap.NewNop(),
	}

	value.PushTypedUserData(l, history, typeHistory)
	l.Push(lua.LNil)
	return 2
}

func makeSnapshotAt(log *zap.Logger) lua.LGoFunc {
	return func(l *lua.LState) int {
		ctx := l.Context()
		if ctx == nil {
			err := lua.NewLuaError(l, "no context").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		reg := regapi.GetRegistry(ctx)
		if reg == nil {
			err := lua.NewLuaError(l, "registry not found in context").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		versionID := l.CheckNumber(1)
		if versionID <= 0 {
			err := lua.NewLuaError(l, "invalid version ID").
				WithKind(lua.Invalid).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		hist := reg.History()
		if hist == nil {
			err := lua.NewLuaError(l, "history not available").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		versions, versErr := hist.Versions()
		if versErr != nil {
			err := lua.WrapErrorWithLua(l, versErr, "get versions").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		var foundVersion regapi.Version
		for _, ver := range versions {
			if ver.ID() == uint(versionID) {
				foundVersion = ver
				break
			}
		}

		if foundVersion == nil {
			err := lua.NewLuaError(l, "version not found").
				WithKind(lua.NotFound).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		resolver := regapi.GetResolver(ctx)
		stateBuilder := topology.NewStateBuilder(log, resolver)

		state, stateErr := stateBuilder.BuildState(hist, foundVersion)
		if stateErr != nil {
			err := lua.WrapErrorWithLua(l, stateErr, "build snapshot state").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		snap := &Snapshot{
			reg:     reg,
			version: foundVersion,
			entries: state,
			log:     log,
		}

		value.PushTypedUserData(l, snap, typeSnapshot)
		l.Push(lua.LNil)
		return 2
	}
}

func registryApplyVersion(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	if !security.IsAllowed(ctx, "registry.apply_version", "", nil) {
		err := lua.NewLuaError(l, "registry version change is not allowed").
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		err := lua.NewLuaError(l, "registry not found in context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	ud := l.CheckUserData(1)
	version, ok := ud.Value.(regapi.Version)
	if !ok {
		err := lua.NewLuaError(l, "expected version object").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	applyErr := reg.ApplyVersion(ctx, version)
	if applyErr != nil {
		err := lua.WrapErrorWithLua(l, applyErr, "apply version").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// Helper functions

var errIDFieldsRequired = errors.New("id requires ns and name fields")

func tableToID(_ *lua.LState, table *lua.LTable) (regapi.ID, error) {
	ns := table.RawGetString("ns")
	name := table.RawGetString("name")

	if ns == lua.LNil || name == lua.LNil {
		return regapi.ID{}, errIDFieldsRequired
	}

	return regapi.NewID(ns.String(), name.String()), nil
}

// tostring helpers

func snapshotToString(l *lua.LState) int {
	snap := checkSnapshot(l)
	if snap == nil {
		l.Push(lua.LString("registry.Snapshot{invalid}"))
		return 1
	}
	l.Push(lua.LString("registry.Snapshot{version=" + snap.version.String() + "}"))
	return 1
}

func changesToString(l *lua.LState) int {
	changes := checkChanges(l)
	if changes == nil {
		l.Push(lua.LString("registry.Changes{invalid}"))
		return 1
	}
	l.Push(lua.LString("registry.Changes{ops=" + strconv.Itoa(len(changes.ops)) + "}"))
	return 1
}

func versionToString(l *lua.LState) int {
	ud := l.CheckUserData(1)
	version, ok := ud.Value.(regapi.Version)
	if !ok {
		l.Push(lua.LString("registry.Version{invalid}"))
		return 1
	}
	l.Push(lua.LString("registry.Version{" + version.String() + "}"))
	return 1
}

func historyToString(l *lua.LState) int {
	l.Push(lua.LString("registry.History{}"))
	return 1
}
