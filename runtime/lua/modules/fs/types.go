// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/runtime/runtime/lua/modules/stream"
)

var streamType typ.Type

func init() {
	if manifest := stream.ModuleTypes(); manifest != nil {
		if t, ok := manifest.LookupType("Stream"); ok {
			streamType = t
		}
	}
	if streamType == nil {
		streamType = typ.Any
	}
}

var fileInfoType = typ.NewRecord().
	Field("name", typ.String).
	Field("size", typ.Number).
	Field("mode", typ.Number).
	Field("modified", typ.Number).
	Field("is_dir", typ.Boolean).
	Field("type", typ.String).
	Build()

var scannerType = typ.NewInterface("fs.Scanner", []typ.Method{
	{Name: "scan", Type: typ.Func().
		Param("self", typ.Self).
		Returns(typ.Boolean).
		Build()},
	{Name: "text", Type: typ.Func().
		Param("self", typ.Self).
		Returns(typ.String).
		Build()},
	{Name: "err", Type: typ.Func().
		Param("self", typ.Self).
		Returns(typ.NewOptional(typ.LuaError)).
		Build()},
})

var fileType = typ.NewInterface("fs.File", []typ.Method{
	{Name: "read", Type: typ.Func().
		Param("self", typ.Self).
		OptParam("n", typ.Number).
		Returns(typ.String, typ.NewOptional(typ.LuaError)).
		Build()},
	{Name: "write", Type: typ.Func().
		Param("self", typ.Self).
		Param("data", typ.String).
		Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
		Build()},
	{Name: "seek", Type: typ.Func().
		Param("self", typ.Self).
		Param("whence", typ.String).
		Param("offset", typ.Number).
		Returns(typ.Number, typ.NewOptional(typ.LuaError)).
		Build()},
	{Name: "close", Type: typ.Func().
		Param("self", typ.Self).
		Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
		Build()},
	{Name: "stat", Type: typ.Func().
		Param("self", typ.Self).
		Returns(fileInfoType, typ.NewOptional(typ.LuaError)).
		Build()},
	{Name: "sync", Type: typ.Func().
		Param("self", typ.Self).
		Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
		Build()},
	{Name: "scanner", Type: typ.Func().
		Param("self", typ.Self).
		OptParam("sep", typ.String).
		Returns(scannerType, typ.NewOptional(typ.LuaError)).
		Build()},
})

var fsType *typ.Interface

func init() {
	fsType = typ.NewInterface("fs.FS", []typ.Method{
		{Name: "chdir", Type: typ.Func().
			Param("self", typ.Self).
			Param("path", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "pwd", Type: typ.Func().
			Param("self", typ.Self).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "open", Type: typ.Func().
			Param("self", typ.Self).
			Param("path", typ.String).
			Param("mode", typ.String).
			Returns(fileType, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "stat", Type: typ.Func().
			Param("self", typ.Self).
			Param("path", typ.String).
			Returns(fileInfoType, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "readdir", Type: typ.Func().
			Param("self", typ.Self).
			Param("path", typ.String).
			Returns(typ.NewArray(fileInfoType), typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "mkdir", Type: typ.Func().
			Param("self", typ.Self).
			Param("path", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "remove", Type: typ.Func().
			Param("self", typ.Self).
			Param("path", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "exists", Type: typ.Func().
			Param("self", typ.Self).
			Param("path", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "isdir", Type: typ.Func().
			Param("self", typ.Self).
			Param("path", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "read_file", Type: typ.Func().
			Param("self", typ.Self).
			Param("path", typ.String).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "readfile", Type: typ.Func().
			Param("self", typ.Self).
			Param("path", typ.String).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "write_file", Type: typ.Func().
			Param("self", typ.Self).
			Param("path", typ.String).
			Param("data", typ.NewUnion(typ.String, streamType)).
			OptParam("mode", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "writefile", Type: typ.Func().
			Param("self", typ.Self).
			Param("path", typ.String).
			Param("data", typ.NewUnion(typ.String, streamType)).
			OptParam("mode", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
	})
}

var seekType = typ.NewRecord().
	Field("SET", typ.String).
	Field("CUR", typ.String).
	Field("END", typ.String).
	Build()

var typeConstType = typ.NewRecord().
	Field("FILE", typ.String).
	Field("DIR", typ.String).
	Build()

func ModuleTypes() *io.Manifest {
	m := io.NewManifest("fs")

	m.DefineType("FS", fsType)
	m.DefineType("File", fileType)
	m.DefineType("FileInfo", fileInfoType)
	m.DefineType("seek", seekType)
	m.DefineType("type", typeConstType)

	moduleFieldsType := typ.NewRecord().
		Field("type", typeConstType).
		Field("seek", seekType).
		Build()

	moduleMethodsType := typ.NewInterface("fs", []typ.Method{
		{Name: "get", Type: typ.Func().
			Param("name", typ.String).
			Returns(fsType, typ.NewOptional(typ.LuaError)).
			Build()},
	})

	m.SetExport(typ.NewIntersection(moduleMethodsType, moduleFieldsType))
	return m
}
