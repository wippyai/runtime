package metrics

import (
	"github.com/yuin/gopher-lua/types/io"
	"github.com/yuin/gopher-lua/types/typ"
)

// ModuleTypes returns the type manifest for the metrics module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("metrics")

	// Labels type accepts any values - runtime ignores non-string values
	labelsType := typ.NewMap(typ.String, typ.Any)

	moduleType := typ.NewInterface("metrics", []typ.Method{
		{Name: "counter_inc", Type: typ.Func().Param("key", typ.String).OptParam("labels", labelsType).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "counter_add", Type: typ.Func().Param("key", typ.String).Param("val", typ.Number).OptParam("labels", labelsType).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "gauge_set", Type: typ.Func().Param("key", typ.String).Param("val", typ.Number).OptParam("labels", labelsType).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "gauge_inc", Type: typ.Func().Param("key", typ.String).OptParam("labels", labelsType).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "gauge_dec", Type: typ.Func().Param("key", typ.String).OptParam("labels", labelsType).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "histogram", Type: typ.Func().Param("key", typ.String).Param("val", typ.Number).OptParam("labels", labelsType).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
