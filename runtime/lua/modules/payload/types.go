package payload

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// Payload type - defined in init to avoid initialization cycle
var payloadType typ.Type

func init() {
	payloadType = typ.NewInterface("payload.Payload", []typ.Method{
		{Name: "get_format", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "data", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "unmarshal", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "transcode", Type: typ.Func().Param("self", typ.Self).Param("format", typ.String).Returns(typ.Self, typ.NewOptional(typ.LuaError)).Build()},
	})
}

// format constants type - using Record for field-only types
var formatType = typ.NewRecord().
	Field("JSON", typ.String).
	Field("YAML", typ.String).
	Field("STRING", typ.String).
	Field("GOLANG", typ.String).
	Field("LUA", typ.String).
	Field("BYTES", typ.String).
	Field("MSGPACK", typ.String).
	Field("ERROR", typ.String).
	Build()

// ModuleTypes returns the type manifest for the payload module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("payload")

	m.DefineType("Payload", payloadType)

	// Module type with fields and methods - use Record for fields
	moduleRecord := typ.NewRecord().
		Field("format", formatType).
		Build()

	// For modules with both fields and methods, we expose the record
	// Methods are handled separately via the interface
	moduleInterface := typ.NewInterface("payload", []typ.Method{
		{Name: "new", Type: typ.Func().Param("data", typ.Any).Returns(payloadType).Build()},
	})

	m.SetExport(typ.NewIntersection(moduleInterface, moduleRecord))
	return m
}
