package store

import (
	"github.com/yuin/gopher-lua/types/io"
	"github.com/yuin/gopher-lua/types/typ"
)

// Store type
var storeType = typ.NewInterface("store.Store", []typ.Method{
	{Name: "get", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "set", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Param("value", typ.Any).OptParam("ttl", typ.Number).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "delete", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "has", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "release", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
})

// ModuleTypes returns the type manifest for the store module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("store")

	m.DefineType("Store", storeType)

	moduleType := typ.NewInterface("store", []typ.Method{
		{Name: "get", Type: typ.Func().Param("name", typ.String).Returns(storeType, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
