// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

var entryType = typ.NewRecord().
	Field("name", typ.String).
	Field("size", typ.Number).
	Field("compressed_size", typ.Number).
	Field("is_dir", typ.Boolean).
	Field("mode", typ.Number).
	Field("modified", typ.Number).
	Field("method", typ.String).
	Field("crc32", typ.Number).
	Field("type", typ.String).
	Build()

var optionsType = typ.NewRecord().
	OptField("format", typ.String).
	OptField("max_entries", typ.Number).
	OptField("max_total_bytes", typ.Number).
	OptField("max_file_bytes", typ.Number).
	OptField("max_inline_bytes", typ.Number).
	OptField("buffer_bytes", typ.Number).
	Build()

var extractOptionsType = typ.NewRecord().
	OptField("prefix", typ.String).
	OptField("strip", typ.Number).
	OptField("filter", typ.Any).
	Build()

var addOptionsType = typ.NewRecord().
	OptField("method", typ.String).
	OptField("size", typ.Number).
	OptField("mode", typ.Number).
	Build()

var readerType = typ.NewInterface("archive.Reader", []typ.Method{
	{Name: "entries", Type: typ.Func().Returns(typ.Any).Build()},
	{Name: "stat", Type: typ.Func().Param("name", typ.String).Returns(entryType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "read", Type: typ.Func().Param("name", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "stream", Type: typ.Func().Param("name", typ.String).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "extract", Type: typ.Func().Param("name", typ.String).Param("dest", typ.Any).OptParam("dest_path", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "extract_all", Type: typ.Func().Param("dest", typ.Any).OptParam("opts", extractOptionsType).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "close", Type: typ.Func().Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
})

var walkerType = typ.NewInterface("archive.Walker", []typ.Method{
	{Name: "walk", Type: typ.Func().Returns(typ.Any).Build()},
	{Name: "extract_all", Type: typ.Func().Param("dest", typ.Any).OptParam("opts", extractOptionsType).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "close", Type: typ.Func().Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
})

var writerType = typ.NewInterface("archive.Writer", []typ.Method{
	{Name: "add", Type: typ.Func().Param("name", typ.String).Param("data", typ.Any).OptParam("opts", addOptionsType).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "add_file", Type: typ.Func().Param("name", typ.String).Param("src", typ.Any).Param("src_path", typ.String).OptParam("opts", addOptionsType).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "add_dir", Type: typ.Func().Param("name", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "close", Type: typ.Func().Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
})

// ModuleTypes returns the type manifest for the archive module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("archive")
	m.DefineType("Entry", entryType)
	m.DefineType("Options", optionsType)
	m.DefineType("ExtractOptions", extractOptionsType)
	m.DefineType("AddOptions", addOptionsType)
	m.DefineType("Reader", readerType)
	m.DefineType("Walker", walkerType)
	m.DefineType("Writer", writerType)

	moduleType := typ.NewRecord().
		Field("open", typ.Func().Param("source", typ.Any).OptParam("a", typ.Any).OptParam("b", typ.Any).Returns(readerType, typ.NewOptional(typ.LuaError)).Build()).
		Field("scan", typ.Func().Param("source", typ.Any).OptParam("opts", optionsType).Returns(walkerType, typ.NewOptional(typ.LuaError)).Build()).
		Field("create", typ.Func().Param("dest", typ.Any).OptParam("a", typ.Any).OptParam("b", typ.Any).Returns(writerType, typ.NewOptional(typ.LuaError)).Build()).
		Field("formats", typ.Func().Returns(typ.NewArray(typ.String)).Build()).
		Build()
	m.SetExport(moduleType)
	return m
}
