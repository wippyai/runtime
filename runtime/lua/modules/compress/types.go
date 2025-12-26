package compress

import (
	"github.com/yuin/gopher-lua/types"
)

// CompressOptions type
var compressOptionsType = &types.RecordType{
	Name: "compress.Options",
	Fields: []types.RecordField{
		{Name: "level", Type: types.Number, Optional: true},
		{Name: "max_size", Type: types.Number, Optional: true},
	},
}

// Codec type for each compression algorithm
var codecType = &types.InterfaceType{
	Name: "compress.Codec",
	Methods: map[string]*types.FunctionType{
		// encode(data: string, opts?: Options): string, Error?
		"encode": types.NewFunction(
			[]types.Type{types.String, types.Optional(compressOptionsType)},
			[]types.Type{types.String, types.Optional(types.LuaError)},
		),
		// decode(data: string, opts?: Options): string, Error?
		"decode": types.NewFunction(
			[]types.Type{types.String, types.Optional(compressOptionsType)},
			[]types.Type{types.String, types.Optional(types.LuaError)},
		),
	},
}

// ModuleTypes returns the type manifest for the compress module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("compress")

	m.DefineType("Options", compressOptionsType)
	m.DefineType("Codec", codecType)

	moduleType := &types.InterfaceType{
		Name: "compress",
		Fields: map[string]types.Type{
			"gzip":    codecType,
			"deflate": codecType,
			"zlib":    codecType,
			"brotli":  codecType,
			"zstd":    codecType,
		},
	}

	m.SetExport(moduleType)
	return m
}
