package compress

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// CompressOptions type
var compressOptionsType = typ.NewRecord().
	OptField("level", typ.Number).
	OptField("max_size", typ.Number).
	Build()

// Codec type for each compression algorithm
var codecType = typ.NewInterface("compress.Codec", []typ.Method{
	{Name: "encode", Type: typ.Func().Param("data", typ.String).OptParam("opts", compressOptionsType).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "decode", Type: typ.Func().Param("data", typ.String).OptParam("opts", compressOptionsType).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
})

// ModuleTypes returns the type manifest for the compress module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("compress")

	m.DefineType("Options", compressOptionsType)
	m.DefineType("Codec", codecType)

	// Module exports codecs as fields (accessed as compress.gzip, compress.zlib, etc.)
	moduleType := typ.NewRecord().
		Field("gzip", codecType).
		Field("deflate", codecType).
		Field("zlib", codecType).
		Field("brotli", codecType).
		Field("zstd", codecType).
		Build()

	m.SetExport(moduleType)
	return m
}
