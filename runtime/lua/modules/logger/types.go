package logger

import (
	"github.com/yuin/gopher-lua/types"
)

// ModuleTypes returns the type manifest for the logger module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("logger")

	// Logger type with self-referencing methods
	loggerType := &types.InterfaceType{
		Name:    "logger.Logger",
		Methods: make(map[string]*types.FunctionType),
	}

	// Add methods after creation to handle self-reference
	loggerType.Methods["debug"] = types.NewFunction([]types.Type{types.String, types.Optional(types.Any)}, nil)
	loggerType.Methods["info"] = types.NewFunction([]types.Type{types.String, types.Optional(types.Any)}, nil)
	loggerType.Methods["warn"] = types.NewFunction([]types.Type{types.String, types.Optional(types.Any)}, nil)
	loggerType.Methods["error"] = types.NewFunction([]types.Type{types.String, types.Optional(types.Any)}, nil)
	loggerType.Methods["with"] = types.NewFunction([]types.Type{types.Any}, []types.Type{loggerType})
	loggerType.Methods["named"] = types.NewFunction([]types.Type{types.String}, []types.Type{loggerType})

	m.DefineType("Logger", loggerType)
	m.SetExport(loggerType)

	return m
}
