// SPDX-License-Identifier: MPL-2.0

package compress

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// CompressOptions type
var compressOptionsType = typ.NewRecord().
	OptField("level", typ.Number).
	OptField("max_size", typ.Number).
	OptField("dict", typ.String).
	Build()

// Codec type for each compression algorithm (no dictionary support).
var codecType = typ.NewInterface("compress.Codec", []typ.Method{
	{Name: "encode", Type: typ.Func().Param("data", typ.String).OptParam("opts", compressOptionsType).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "decode", Type: typ.Func().Param("data", typ.String).OptParam("opts", compressOptionsType).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
})

// Zstd train_dict options.
var zstdTrainDictOptionsType = typ.NewRecord().
	OptField("size", typ.Number).
	OptField("id", typ.Number).
	OptField("level", typ.Number).
	Build()

// Zstd inspect_dict result.
var zstdDictInfoType = typ.NewRecord().
	Field("id", typ.Number).
	Field("content_size", typ.Number).
	Build()

// Zstd codec extends Codec with dict training and inspection.
var zstdCodecType = typ.NewInterface("compress.ZstdCodec", []typ.Method{
	{Name: "encode", Type: typ.Func().Param("data", typ.String).OptParam("opts", compressOptionsType).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "decode", Type: typ.Func().Param("data", typ.String).OptParam("opts", compressOptionsType).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "train_dict", Type: typ.Func().Param("samples", typ.NewArray(typ.String)).OptParam("opts", zstdTrainDictOptionsType).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "inspect_dict", Type: typ.Func().Param("dict", typ.String).Returns(zstdDictInfoType, typ.NewOptional(typ.LuaError)).Build()},
})

// ModuleTypes returns the type manifest for the compress module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("compress")

	m.DefineType("Options", compressOptionsType)
	m.DefineType("Codec", codecType)
	m.DefineType("ZstdCodec", zstdCodecType)
	m.DefineType("ZstdTrainDictOptions", zstdTrainDictOptionsType)
	m.DefineType("ZstdDictInfo", zstdDictInfoType)

	// Module exports codecs as fields (accessed as compress.gzip, compress.zlib, etc.)
	moduleType := typ.NewRecord().
		Field("gzip", codecType).
		Field("deflate", codecType).
		Field("zlib", codecType).
		Field("brotli", codecType).
		Field("zstd", zstdCodecType).
		Build()

	m.SetExport(moduleType)
	return m
}
