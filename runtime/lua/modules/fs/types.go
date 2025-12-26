package fs

import (
	"github.com/yuin/gopher-lua/types"
)

// FileInfo type
var fileInfoType = &types.RecordType{
	Name: "fs.FileInfo",
	Fields: []types.RecordField{
		{Name: "name", Type: types.String},
		{Name: "size", Type: types.Number},
		{Name: "mode", Type: types.Number},
		{Name: "modified", Type: types.Number},
		{Name: "is_dir", Type: types.Boolean},
		{Name: "type", Type: types.String},
	},
}

// File userdata type
var fileType = &types.InterfaceType{
	Name: "fs.File",
	Methods: map[string]*types.FunctionType{
		"read":     types.NewFunction([]types.Type{types.Optional(types.Number)}, []types.Type{types.String, types.Optional(types.LuaError)}),
		"read_all": types.NewFunction(nil, []types.Type{types.String, types.Optional(types.LuaError)}),
		"write":    types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"seek":     types.NewFunction([]types.Type{types.String, types.Number}, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"close":    types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"stat":     types.NewFunction(nil, []types.Type{fileInfoType, types.Optional(types.LuaError)}),
		"lines":    types.NewFunction(nil, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"sync":     types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
	},
}

// Forward declaration for self-referential FS type
var fsType *types.InterfaceType

func init() {
	fsType = &types.InterfaceType{
		Name:    "fs.FS",
		Methods: map[string]*types.FunctionType{},
	}
	fsType.Methods["chdir"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)})
	fsType.Methods["pwd"] = types.NewFunction(nil, []types.Type{types.String, types.Optional(types.LuaError)})
	fsType.Methods["open"] = types.NewFunction([]types.Type{types.String, types.Optional(types.String)}, []types.Type{fileType, types.Optional(types.LuaError)})
	fsType.Methods["stat"] = types.NewFunction([]types.Type{types.String}, []types.Type{fileInfoType, types.Optional(types.LuaError)})
	fsType.Methods["read_dir"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.NewArray(fileInfoType, false), types.Optional(types.LuaError)})
	fsType.Methods["readdir"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.NewArray(fileInfoType, false), types.Optional(types.LuaError)})
	fsType.Methods["mkdir"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)})
	fsType.Methods["mkdir_all"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)})
	fsType.Methods["remove"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)})
	fsType.Methods["remove_all"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)})
	fsType.Methods["rename"] = types.NewFunction([]types.Type{types.String, types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)})
	fsType.Methods["exists"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)})
	fsType.Methods["isdir"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)})
	fsType.Methods["glob"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.NewArray(types.String, false), types.Optional(types.LuaError)})
	fsType.Methods["read_file"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.String, types.Optional(types.LuaError)})
	fsType.Methods["readfile"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.String, types.Optional(types.LuaError)})
	writeModeType := types.Optional(types.NewUnion(types.NewLiteral("w"), types.NewLiteral("wx"), types.NewLiteral("a")))
	fsType.Methods["write_file"] = types.NewFunction([]types.Type{types.String, types.String, writeModeType}, []types.Type{types.Boolean, types.Optional(types.LuaError)})
	fsType.Methods["writefile"] = types.NewFunction([]types.Type{types.String, types.String, writeModeType}, []types.Type{types.Boolean, types.Optional(types.LuaError)})
	fsType.Methods["copy"] = types.NewFunction([]types.Type{types.String, types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)})
	fsType.Methods["sub"] = types.NewFunction([]types.Type{types.String}, []types.Type{fsType, types.Optional(types.LuaError)})
}

// Seek constants type
var seekType = &types.InterfaceType{
	Name: "fs.seek",
	Fields: map[string]types.Type{
		"SET": types.String,
		"CUR": types.String,
		"END": types.String,
	},
}

// Type constants type
var typeConstType = &types.InterfaceType{
	Name: "fs.type",
	Fields: map[string]types.Type{
		"FILE": types.String,
		"DIR":  types.String,
	},
}

// ModuleTypes returns the type manifest for the fs module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("fs")

	m.DefineType("FS", fsType)
	m.DefineType("File", fileType)
	m.DefineType("FileInfo", fileInfoType)

	moduleType := &types.InterfaceType{
		Name: "fs",
		Fields: map[string]types.Type{
			"type": typeConstType,
			"seek": seekType,
		},
		Methods: map[string]*types.FunctionType{
			"get": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{fsType, types.Optional(types.LuaError)},
			),
		},
	}

	m.SetExport(moduleType)
	return m
}
