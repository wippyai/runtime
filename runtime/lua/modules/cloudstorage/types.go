// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ListObjectsOptions type
var listObjectsOptionsType = typ.NewRecord().
	OptField("prefix", typ.String).
	OptField("max_keys", typ.Number).
	OptField("continuation_token", typ.String).
	Build()

// ListObjectsResult type
var listObjectsResultType = typ.NewRecord().
	Field("objects", typ.NewArray(typ.Any)).
	OptField("continuation_token", typ.String).
	Field("is_truncated", typ.Boolean).
	Build()

// DownloadOptions type
var downloadOptionsType = typ.NewRecord().
	OptField("range", typ.String).
	Build()

// PresignedURLOptions type
var presignedURLOptionsType = typ.NewRecord().
	OptField("expiration", typ.Number).
	OptField("content_type", typ.String).
	OptField("content_length", typ.Number).
	Build()

// Storage type
var storageType = typ.NewInterface("cloudstorage.Storage", []typ.Method{
	{Name: "list_objects", Type: typ.Func().Param("self", typ.Self).OptParam("options", listObjectsOptionsType).Returns(listObjectsResultType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "download_object", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Param("dest", typ.Any).OptParam("options", downloadOptionsType).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "upload_object", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Param("source", typ.Any).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "delete_objects", Type: typ.Func().Param("self", typ.Self).Param("keys", typ.NewArray(typ.String)).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "presigned_get_url", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).OptParam("options", presignedURLOptionsType).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "presigned_put_url", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).OptParam("options", presignedURLOptionsType).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "release", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
})

// ModuleTypes returns the type manifest for the cloudstorage module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("cloudstorage")

	m.DefineType("Storage", storageType)
	m.DefineType("ListObjectsOptions", listObjectsOptionsType)
	m.DefineType("ListObjectsResult", listObjectsResultType)
	m.DefineType("DownloadOptions", downloadOptionsType)
	m.DefineType("PresignedURLOptions", presignedURLOptionsType)

	moduleType := typ.NewInterface("cloudstorage", []typ.Method{
		{Name: "get", Type: typ.Func().Param("name", typ.String).Returns(storageType, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
