// SPDX-License-Identifier: MPL-2.0

package stream

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// StreamType is exported for use by other modules (fs, http).
var StreamType = typ.NewInterface("stream.Stream", []typ.Method{
	{Name: "read", Type: typ.Func().Param("self", typ.Self).OptParam("n", typ.Number).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "write", Type: typ.Func().Param("self", typ.Self).Param("data", typ.String).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "seek", Type: typ.Func().Param("self", typ.Self).OptParam("whence", typ.String).OptParam("offset", typ.Number).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "flush", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "stat", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "close", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "scanner", Type: typ.Func().Param("self", typ.Self).OptParam("delim", typ.String).Returns(scannerType, typ.NewOptional(typ.LuaError)).Build()},
})

// Scanner type
var scannerType = typ.NewInterface("stream.Scanner", []typ.Method{
	{Name: "scan", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "text", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
	{Name: "err", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.LuaError)).Build()},
})

// ModuleTypes returns the type manifest for the stream module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("stream")

	m.DefineType("Stream", StreamType)
	m.DefineType("Scanner", scannerType)

	moduleType := typ.NewInterface("stream", []typ.Method{})

	m.SetExport(moduleType)
	return m
}
