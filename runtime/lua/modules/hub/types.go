// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ModuleTypes returns the type manifest for the hub module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("hub")

	listResponse := typ.NewRecord().
		Field("items", typ.Any).
		Field("total", typ.Number).
		Field("page", typ.Number).
		Field("page_size", typ.Number).
		Build()

	itemsResponse := typ.NewRecord().
		Field("items", typ.Any).
		Build()

	readmeResponse := typ.NewRecord().
		Field("content", typ.String).
		Field("filename", typ.String).
		Field("version", typ.String).
		Build()

	modulesIface := typ.NewInterface("hub.modules", []typ.Method{
		{Name: "list", Type: typ.Func().OptParam("opts", typ.Any).Returns(listResponse, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "search", Type: typ.Func().Param("query", typ.String).OptParam("opts", typ.Any).Returns(listResponse, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "get", Type: typ.Func().Param("module", typ.Any).OptParam("opts", typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "readme", Type: typ.Func().Param("module", typ.Any).OptParam("opts", typ.Any).Returns(readmeResponse, typ.NewOptional(typ.LuaError)).Build()},
	})

	versionsIface := typ.NewInterface("hub.versions", []typ.Method{
		{Name: "list", Type: typ.Func().Param("module", typ.Any).OptParam("opts", typ.Any).Returns(listResponse, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "get", Type: typ.Func().Param("module", typ.Any).Param("version", typ.Any).OptParam("opts", typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "inspect", Type: typ.Func().Param("module", typ.Any).Param("version", typ.Any).OptParam("opts", typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	})

	dependenciesIface := typ.NewInterface("hub.dependencies", []typ.Method{
		{Name: "get", Type: typ.Func().Param("module", typ.Any).OptParam("version", typ.Any).OptParam("opts", typ.Any).Returns(itemsResponse, typ.NewOptional(typ.LuaError)).Build()},
	})

	dependentsIface := typ.NewInterface("hub.dependents", []typ.Method{
		{Name: "get", Type: typ.Func().Param("module", typ.Any).OptParam("opts", typ.Any).Returns(listResponse, typ.NewOptional(typ.LuaError)).Build()},
	})

	filesIface := typ.NewInterface("hub.files", []typ.Method{
		{Name: "list", Type: typ.Func().Param("module", typ.Any).Param("version", typ.Any).OptParam("opts", typ.Any).Returns(listResponse, typ.NewOptional(typ.LuaError)).Build()},
	})

	moduleType := typ.NewRecord().
		Field("modules", modulesIface).
		Field("versions", versionsIface).
		Field("dependencies", dependenciesIface).
		Field("dependents", dependentsIface).
		Field("files", filesIface).
		Build()

	m.SetExport(moduleType)
	return m
}
