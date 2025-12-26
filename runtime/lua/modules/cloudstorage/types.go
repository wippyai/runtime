package cloudstorage

import (
	"github.com/yuin/gopher-lua/types"
)

// ListObjectsOptions type (anonymous readonly for structural covariant typing)
var listObjectsOptionsType = &types.RecordType{
	Readonly: true,
	Fields: []types.RecordField{
		{Name: "prefix", Type: types.Optional(types.String), Optional: true},
		{Name: "max_keys", Type: types.Optional(types.Number), Optional: true},
		{Name: "continuation_token", Type: types.Optional(types.String), Optional: true},
	},
}

// ListObjectsResult type
var listObjectsResultType = &types.RecordType{
	Name: "cloudstorage.ListObjectsResult",
	Fields: []types.RecordField{
		{Name: "objects", Type: types.NewArray(types.Any, false)},
		{Name: "continuation_token", Type: types.Optional(types.String)},
		{Name: "is_truncated", Type: types.Boolean},
	},
}

// DownloadOptions type (anonymous readonly for structural covariant typing)
var downloadOptionsType = &types.RecordType{
	Readonly: true,
	Fields: []types.RecordField{
		{Name: "range", Type: types.Optional(types.String), Optional: true},
	},
}

// PresignedURLOptions type (anonymous readonly for structural covariant typing)
var presignedURLOptionsType = &types.RecordType{
	Readonly: true,
	Fields: []types.RecordField{
		{Name: "expiration", Type: types.Optional(types.Number), Optional: true},
		{Name: "content_type", Type: types.Optional(types.String), Optional: true},
		{Name: "content_length", Type: types.Optional(types.Number), Optional: true},
	},
}

// Storage type
var storageType = &types.InterfaceType{
	Name: "cloudstorage.Storage",
	Methods: map[string]*types.FunctionType{
		"list_objects":      types.NewFunction([]types.Type{types.Optional(listObjectsOptionsType)}, []types.Type{listObjectsResultType, types.Optional(types.LuaError)}),
		"download_object":   types.NewFunction([]types.Type{types.String, types.Any, types.Optional(downloadOptionsType)}, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"upload_object":     types.NewFunction([]types.Type{types.String, types.Any}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"delete_objects":    types.NewFunction([]types.Type{types.NewArray(types.String, false)}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"presigned_get_url": types.NewFunction([]types.Type{types.String, types.Optional(presignedURLOptionsType)}, []types.Type{types.String, types.Optional(types.LuaError)}),
		"presigned_put_url": types.NewFunction([]types.Type{types.String, types.Optional(presignedURLOptionsType)}, []types.Type{types.String, types.Optional(types.LuaError)}),
		"release":           types.NewFunction(nil, []types.Type{types.Boolean}),
	},
}

// ModuleTypes returns the type manifest for the cloudstorage module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("cloudstorage")

	m.DefineType("Storage", storageType)
	m.DefineType("ListObjectsOptions", listObjectsOptionsType)
	m.DefineType("ListObjectsResult", listObjectsResultType)
	m.DefineType("DownloadOptions", downloadOptionsType)
	m.DefineType("PresignedURLOptions", presignedURLOptionsType)

	moduleType := &types.InterfaceType{
		Name: "cloudstorage",
		Methods: map[string]*types.FunctionType{
			"get": types.NewFunction([]types.Type{types.String}, []types.Type{storageType, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
