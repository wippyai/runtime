package process

import (
	"github.com/yuin/gopher-lua/types"
)

// Message type
var messageType = &types.InterfaceType{
	Name: "process.Message",
	Methods: map[string]*types.FunctionType{
		"from":    types.NewFunction([]types.Type{types.Self}, []types.Type{types.String}),
		"topic":   types.NewFunction([]types.Type{types.Self}, []types.Type{types.String}),
		"payload": types.NewFunction([]types.Type{types.Self}, []types.Type{types.Any}),
	},
}

// ProcessOptions type
var processOptionsType = &types.RecordType{
	Name: "process.Options",
	Fields: []types.RecordField{
		{Name: "trap_links", Type: types.Boolean},
	},
}

// event constants type
var eventType = &types.InterfaceType{
	Name: "process.event",
	Fields: map[string]types.Type{
		"CANCEL":    types.String,
		"EXIT":      types.String,
		"LINK_DOWN": types.String,
	},
}

// registry submodule type
var registrySubType = &types.InterfaceType{
	Name: "process.registry",
	Methods: map[string]*types.FunctionType{
		"register":   types.NewFunction([]types.Type{types.String, types.Optional(types.String)}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"lookup":     types.NewFunction([]types.Type{types.String}, []types.Type{types.String, types.Optional(types.LuaError)}),
		"unregister": types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean}),
	},
}

// Forward declaration for self-referential SpawnBuilder
var spawnBuilderType *types.InterfaceType

func init() {
	spawnBuilderType = &types.InterfaceType{
		Name:    "process.SpawnBuilder",
		Methods: map[string]*types.FunctionType{},
	}
	spawnBuilderType.Methods["with_context"] = types.NewFunction([]types.Type{types.Self, types.NewMap(types.String, types.Any, true)}, []types.Type{spawnBuilderType})
	spawnBuilderType.Methods["with_actor"] = types.NewFunction([]types.Type{types.Self, types.Any}, []types.Type{spawnBuilderType})
	spawnBuilderType.Methods["with_scope"] = types.NewFunction([]types.Type{types.Self, types.Any}, []types.Type{spawnBuilderType})
	spawnBuilderType.Methods["with_name"] = types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{spawnBuilderType})
	spawnBuilderType.Methods["with_message"] = &types.FunctionType{Params: []types.Type{types.Self, types.String}, Variadic: types.Any, Returns: []types.Type{spawnBuilderType}}
	spawnBuilderType.Methods["spawn"] = &types.FunctionType{Params: []types.Type{types.Self, types.String, types.String}, Variadic: types.Any, Returns: []types.Type{types.String, types.Optional(types.LuaError)}}
	spawnBuilderType.Methods["spawn_monitored"] = &types.FunctionType{Params: []types.Type{types.Self, types.String, types.String}, Variadic: types.Any, Returns: []types.Type{types.String, types.Optional(types.LuaError)}}
	spawnBuilderType.Methods["spawn_linked"] = &types.FunctionType{Params: []types.Type{types.Self, types.String, types.String}, Variadic: types.Any, Returns: []types.Type{types.String, types.Optional(types.LuaError)}}
	spawnBuilderType.Methods["spawn_linked_monitored"] = &types.FunctionType{Params: []types.Type{types.Self, types.String, types.String}, Variadic: types.Any, Returns: []types.Type{types.String, types.Optional(types.LuaError)}}
}

// ModuleTypes returns the type manifest for the process module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("process")

	m.DefineType("Message", messageType)
	m.DefineType("Options", processOptionsType)
	m.DefineType("SpawnBuilder", spawnBuilderType)

	moduleType := &types.InterfaceType{
		Name: "process",
		Fields: map[string]types.Type{
			"event":    eventType,
			"registry": registrySubType,
		},
		Methods: map[string]*types.FunctionType{
			"id":                     types.NewFunction(nil, []types.Type{types.String, types.Optional(types.String)}),
			"pid":                    types.NewFunction(nil, []types.Type{types.String}),
			"send":                   {Params: []types.Type{types.String, types.String}, Variadic: types.Any, Returns: []types.Type{types.Boolean, types.Optional(types.String)}},
			"spawn":                  {Params: []types.Type{types.String, types.String}, Variadic: types.Any, Returns: []types.Type{types.String, types.Optional(types.String)}},
			"spawn_monitored":        {Params: []types.Type{types.String, types.String}, Variadic: types.Any, Returns: []types.Type{types.String, types.Optional(types.String)}},
			"spawn_linked":           {Params: []types.Type{types.String, types.String}, Variadic: types.Any, Returns: []types.Type{types.String, types.Optional(types.String)}},
			"spawn_linked_monitored": {Params: []types.Type{types.String, types.String}, Variadic: types.Any, Returns: []types.Type{types.String, types.Optional(types.String)}},
			"terminate":              types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.String)}),
			"cancel":                 types.NewFunction([]types.Type{types.String, types.Optional(types.Any)}, []types.Type{types.Boolean, types.Optional(types.String)}),
			"get_options":            types.NewFunction(nil, []types.Type{processOptionsType}),
			"set_options":            types.NewFunction([]types.Type{processOptionsType}, []types.Type{types.Boolean, types.Optional(types.String)}),
			"monitor":                types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.String)}),
			"unmonitor":              types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.String)}),
			"link":                   types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.String)}),
			"unlink":                 types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.String)}),
			"with_context":           types.NewFunction([]types.Type{types.NewMap(types.String, types.Any, true)}, []types.Type{spawnBuilderType}),
			"inbox":                  types.NewFunction(nil, []types.Type{types.Any, types.Optional(types.String)}),
			"events":                 types.NewFunction(nil, []types.Type{types.Any, types.Optional(types.String)}),
			"listen":                 types.NewFunction([]types.Type{types.String, types.Optional(types.Any)}, []types.Type{types.Any, types.Optional(types.String)}),
			"unlisten":               types.NewFunction([]types.Type{types.Any}, []types.Type{types.Boolean, types.Optional(types.String)}),
			"upgrade":                {Params: []types.Type{types.Optional(types.String)}, Variadic: types.Any, Returns: []types.Type{types.Boolean, types.Optional(types.String)}},
			"run":                    {Params: []types.Type{types.String, types.String}, Variadic: types.Any, Returns: []types.Type{types.Any, types.Optional(types.String)}},
		},
	}

	m.SetExport(moduleType)
	return m
}
