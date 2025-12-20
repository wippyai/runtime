package registry

import "github.com/yuin/gopher-lua/types"

// ID record type
var idType = &types.RecordType{
	Name: "registry.ID",
	Fields: []types.RecordField{
		{Name: "ns", Type: types.String},
		{Name: "name", Type: types.String},
	},
}

// Forward declarations for self-referential types
var (
	snapshotType *types.InterfaceType
	changesType  *types.InterfaceType
	versionType  *types.InterfaceType
	historyType  *types.InterfaceType
)

func init() {
	// Version type (self-referential via previous)
	versionType = &types.InterfaceType{
		Name:    "registry.Version",
		Methods: map[string]*types.FunctionType{},
	}
	versionType.Methods["id"] = types.NewFunction(nil, []types.Type{types.Number})
	versionType.Methods["previous"] = types.NewFunction(nil, []types.Type{types.Optional(versionType)})
	versionType.Methods["string"] = types.NewFunction(nil, []types.Type{types.String})

	// Changes type (self-referential via create/update/delete)
	changesType = &types.InterfaceType{
		Name:    "registry.Changes",
		Methods: map[string]*types.FunctionType{},
	}
	changesType.Methods["ops"] = types.NewFunction(nil, []types.Type{types.NewArray(types.Any, false)})
	changesType.Methods["create"] = types.NewFunction([]types.Type{types.String, types.Any}, []types.Type{changesType})
	changesType.Methods["update"] = types.NewFunction([]types.Type{types.String, types.Any}, []types.Type{changesType})
	changesType.Methods["delete"] = types.NewFunction([]types.Type{types.String}, []types.Type{changesType})
	changesType.Methods["apply"] = types.NewFunction(nil, []types.Type{versionType, types.Optional(types.LuaError)})

	// Snapshot type (references changesType and versionType)
	snapshotType = &types.InterfaceType{
		Name: "registry.Snapshot",
		Methods: map[string]*types.FunctionType{
			"entries":   types.NewFunction(nil, []types.Type{types.NewArray(types.Any, false)}),
			"get":       types.NewFunction([]types.Type{types.String}, []types.Type{types.Any, types.Optional(types.LuaError)}),
			"namespace": types.NewFunction([]types.Type{types.String}, []types.Type{types.NewArray(types.Any, false)}),
			"find":      types.NewFunction([]types.Type{types.Any}, []types.Type{types.NewArray(types.Any, false)}),
			"changes":   types.NewFunction(nil, []types.Type{changesType, types.Optional(types.LuaError)}),
			"version":   types.NewFunction(nil, []types.Type{versionType}),
		},
	}

	// History type (references versionType and snapshotType)
	historyType = &types.InterfaceType{
		Name: "registry.History",
		Methods: map[string]*types.FunctionType{
			"versions":    types.NewFunction(nil, []types.Type{types.NewArray(versionType, false), types.Optional(types.LuaError)}),
			"get_version": types.NewFunction([]types.Type{types.Number}, []types.Type{versionType, types.Optional(types.LuaError)}),
			"snapshot_at": types.NewFunction([]types.Type{types.Number}, []types.Type{snapshotType, types.Optional(types.LuaError)}),
		},
	}
}

// ModuleTypes returns the type manifest for the registry module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("registry")

	m.DefineType("Snapshot", snapshotType)
	m.DefineType("Changes", changesType)
	m.DefineType("Version", versionType)
	m.DefineType("History", historyType)
	m.DefineType("ID", idType)

	moduleType := &types.InterfaceType{
		Name: "registry",
		Methods: map[string]*types.FunctionType{
			"get":             types.NewFunction([]types.Type{types.String}, []types.Type{types.Any, types.Optional(types.LuaError)}),
			"find":            types.NewFunction([]types.Type{types.Any}, []types.Type{types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
			"parse_id":        types.NewFunction([]types.Type{types.String}, []types.Type{idType}),
			"snapshot":        types.NewFunction(nil, []types.Type{snapshotType, types.Optional(types.LuaError)}),
			"snapshot_at":     types.NewFunction([]types.Type{types.Number}, []types.Type{snapshotType, types.Optional(types.LuaError)}),
			"current_version": types.NewFunction(nil, []types.Type{versionType, types.Optional(types.LuaError)}),
			"versions":        types.NewFunction(nil, []types.Type{types.NewArray(versionType, false), types.Optional(types.LuaError)}),
			"history":         types.NewFunction(nil, []types.Type{historyType, types.Optional(types.LuaError)}),
			"apply_version":   types.NewFunction([]types.Type{versionType}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"build_delta":     types.NewFunction([]types.Type{versionType, versionType}, []types.Type{types.Any, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
