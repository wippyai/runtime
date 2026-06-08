// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

var sourceInfoType = typ.NewRecord().
	Field("name", typ.String).
	Field("slot", typ.String).
	Field("event_system", typ.String).
	OptField("publication", typ.String).
	OptField("tables", typ.NewArray(typ.String)).
	Field("streaming", typ.Boolean).
	Field("failover", typ.Boolean).
	Field("temporary", typ.Boolean).
	Field("snapshot", typ.Boolean).
	Build()

func ModuleTypes() *io.Manifest {
	m := io.NewManifest("cdc")

	moduleType := typ.NewInterface("cdc", []typ.Method{
		{Name: "list_sources", Type: typ.Func().Returns(typ.NewArray(sourceInfoType), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "source", Type: typ.Func().Param("name", typ.String).Returns(typ.NewOptional(sourceInfoType), typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
