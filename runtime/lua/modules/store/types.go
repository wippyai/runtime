// SPDX-License-Identifier: MPL-2.0

package store

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

var backendConstType = typ.NewRecord().
	Field("KV_RAFT", typ.String).
	Field("KV_CRDT", typ.String).
	Field("MEMORY", typ.String).
	Field("SQL", typ.String).
	Field("UNKNOWN", typ.String).
	Build()

var consistencyConstType = typ.NewRecord().
	Field("LINEARIZABLE", typ.String).
	Field("EVENTUAL", typ.String).
	Field("LOCAL", typ.String).
	Field("UNKNOWN", typ.String).
	Build()

var storeInfoType = typ.NewRecord().
	Field("id", typ.String).
	Field("backend", typ.String).
	Field("consistency", typ.String).
	Field("durable", typ.Boolean).
	Field("list", typ.Boolean).
	Field("versioned", typ.Boolean).
	Field("conditional_put", typ.Boolean).
	Field("ttl", typ.Boolean).
	Build()

var storeEntryType = typ.NewRecord().
	Field("key", typ.String).
	Field("value", typ.Any).
	Field("version", typ.String).
	Build()

var storePageType = typ.NewRecord().
	Field("items", typ.NewArray(storeEntryType)).
	Field("cursor", typ.String).
	Field("has_more", typ.Boolean).
	Build()

var listOptionsType = typ.NewRecord().
	OptField("prefix", typ.String).
	OptField("after", typ.String).
	OptField("limit", typ.Integer).
	Build()

var putOptionsType = typ.NewRecord().
	OptField("ttl", typ.Number).
	OptField("only_if_absent", typ.Boolean).
	OptField("if_version", typ.String).
	Build()

// Store type
var storeType = typ.NewInterface("store.Store", []typ.Method{
	{Name: "info", Type: typ.Func().Param("self", typ.Self).Returns(storeInfoType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "get", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "entry", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Returns(storeEntryType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "list", Type: typ.Func().Param("self", typ.Self).OptParam("opts", listOptionsType).Returns(storePageType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "put", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Param("value", typ.Any).OptParam("opts", putOptionsType).Returns(storeEntryType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "set", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Param("value", typ.Any).OptParam("ttl", typ.Number).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "delete", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "has", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "release", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
})

// ModuleTypes returns the type manifest for the store module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("store")

	m.DefineType("Store", storeType)
	m.DefineType("Info", storeInfoType)
	m.DefineType("Entry", storeEntryType)
	m.DefineType("Page", storePageType)
	m.DefineType("ListOptions", listOptionsType)
	m.DefineType("PutOptions", putOptionsType)

	moduleMethodsType := typ.NewInterface("store", []typ.Method{
		{Name: "get", Type: typ.Func().Param("name", typ.String).Returns(storeType, typ.NewOptional(typ.LuaError)).Build()},
	})
	moduleFieldsType := typ.NewRecord().
		Field("backend", backendConstType).
		Field("consistency", consistencyConstType).
		Build()

	m.SetExport(typ.NewIntersection(moduleMethodsType, moduleFieldsType))
	return m
}
