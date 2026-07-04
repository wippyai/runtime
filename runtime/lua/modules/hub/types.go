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

	// Base call options shared by the artifact-reading methods.
	baseOpts := typ.NewRecord().
		OptField("registry", typ.String).
		OptField("token", typ.String).
		OptField("timeout", typ.Number).
		Build()

	// meta and data carry decoded config verbatim; their leaf values are
	// arbitrary, matching how the registry module types entries.
	metaMap := typ.NewMap(typ.String, typ.Any)

	packageEntryType := typ.NewRecord().
		Field("id", typ.String).
		Field("kind", typ.String).
		Field("meta", metaMap).
		OptField("data", typ.Any).
		Build()

	packageResourceType := typ.NewRecord().
		Field("id", typ.String).
		Field("type", typ.String).
		Field("hash", typ.String).
		Field("size", typ.Number).
		Field("file_count", typ.Number).
		Field("meta", metaMap).
		Build()

	packageEntriesOpts := typ.NewRecord().
		OptField("kind", typ.NewUnion(typ.String, typ.NewArray(typ.String))).
		OptField("include_data", typ.Boolean).
		Build()

	// The package handle from hub.versions.open: scalar fields plus methods
	// resolved through the metatable.
	packageMethods := typ.NewInterface("hub.Package", []typ.Method{
		{Name: "metadata", Type: typ.Func().Param("self", typ.Self).Returns(metaMap, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "entries", Type: typ.Func().Param("self", typ.Self).OptParam("opts", packageEntriesOpts).Returns(typ.NewArray(packageEntryType), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "resources", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewArray(packageResourceType), typ.NewOptional(typ.LuaError)).Build()},
		// fs returns the shared fs module handle (a foreign userdata).
		{Name: "fs", Type: typ.Func().Param("self", typ.Self).Param("resource", typ.String).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "close", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	})

	packageType := typ.NewRecord().
		ReadonlyField("version", typ.String).
		ReadonlyField("digest", typ.String).
		ReadonlyField("packed", typ.Boolean).
		Metatable(packageMethods).
		Build()

	cacheEntryType := typ.NewRecord().
		Field("module", typ.String).
		Field("version", typ.String).
		Field("size", typ.Number).
		Field("pinned", typ.Boolean).
		Build()
	cacheList := typ.NewArray(cacheEntryType)

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
		{Name: "open", Type: typ.Func().Param("module", typ.Any).Param("version", typ.Any).OptParam("opts", baseOpts).Returns(packageType, typ.NewOptional(typ.LuaError)).Build()},
	})

	cacheRemoveOpts := typ.NewRecord().OptField("force", typ.Boolean).Build()
	cachePruneOpts := typ.NewRecord().OptField("dry_run", typ.Boolean).Build()
	cacheIface := typ.NewInterface("hub.cache", []typ.Method{
		{Name: "list", Type: typ.Func().OptParam("opts", baseOpts).Returns(cacheList, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "remove", Type: typ.Func().Param("module", typ.String).Param("version", typ.String).OptParam("opts", cacheRemoveOpts).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "prune", Type: typ.Func().OptParam("opts", cachePruneOpts).Returns(cacheList, typ.NewOptional(typ.LuaError)).Build()},
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

	authStatus := typ.NewRecord().
		Field("authenticated", typ.Boolean).
		Field("registry", typ.String).
		Field("orgs", typ.Any).
		Build()

	authIface := typ.NewInterface("hub.auth", []typ.Method{
		{Name: "authenticate", Type: typ.Func().Param("token", typ.String).OptParam("registry", typ.String).Returns(authStatus, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "logout", Type: typ.Func().OptParam("registry", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "status", Type: typ.Func().OptParam("registry", typ.String).Returns(authStatus, typ.NewOptional(typ.LuaError)).Build()},
	})

	moduleType := typ.NewRecord().
		Field("modules", modulesIface).
		Field("versions", versionsIface).
		Field("dependencies", dependenciesIface).
		Field("dependents", dependentsIface).
		Field("files", filesIface).
		Field("auth", authIface).
		Field("cache", cacheIface).
		Build()

	m.SetExport(moduleType)
	return m
}
