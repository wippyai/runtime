// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

var cdcChannelType typ.Type

var sourceCapabilitiesType = typ.NewRecord().
	Field("snapshot", typ.Boolean).
	Field("durable", typ.Boolean).
	Field("replayable", typ.Boolean).
	Field("captures_external_writes", typ.Boolean).
	Field("before_images", typ.Boolean).
	Field("coalesced", typ.Boolean).
	Build()

var sourceInfoType = typ.NewRecord().
	OptField("id", typ.String).
	OptField("kind", typ.String).
	Field("state", typ.String).
	OptField("generation", typ.String).
	Field("capabilities", sourceCapabilitiesType).
	Field("name", typ.String).
	Field("slot", typ.String).
	OptField("publication", typ.String).
	OptField("engine", typ.String).
	OptField("file", typ.String).
	OptField("db_resource", typ.String).
	OptField("epoch", typ.String).
	OptField("error", typ.String).
	OptField("tables", typ.NewArray(typ.String)).
	Field("streaming", typ.Boolean).
	Field("failover", typ.Boolean).
	Field("temporary", typ.Boolean).
	Field("snapshot", typ.Boolean).
	Field("faulted", typ.Boolean).
	Build()

var streamOptionsType = typ.NewRecord().
	OptField("tables", typ.NewArray(typ.String)).
	OptField("ops", typ.NewArray(typ.String)).
	OptField("buffer", typ.Integer).
	OptField("max_bytes", typ.Integer).
	OptField("snapshot", typ.Boolean).
	OptField("after", typ.String).
	Build()

var changeType = typ.NewRecord().
	OptField("source_id", typ.String).
	Field("source", typ.String).
	Field("op", typ.String).
	Field("schema", typ.String).
	Field("table", typ.String).
	Field("relation", typ.String).
	Field("lsn", typ.String).
	OptField("commit_lsn", typ.String).
	OptField("cursor", typ.String).
	OptField("generation", typ.String).
	OptField("transaction", typ.String).
	OptField("error", typ.String).
	OptField("xid", typ.Integer).
	OptField("before", typ.NewMap(typ.String, typ.Any)).
	OptField("after", typ.NewMap(typ.String, typ.Any)).
	Build()

var cdcStreamType *typ.Interface

func init() {
	cdcChannelType = typ.Any
	if manifest := engine.ChannelModuleTypes(); manifest != nil {
		if t, ok := manifest.LookupType("Channel"); ok {
			if gen, ok := t.(*typ.Generic); ok {
				cdcChannelType = typ.Instantiate(gen, changeType)
			}
		}
	}

	cdcStreamType = typ.NewInterface("cdc.Stream", []typ.Method{
		{Name: "channel", Type: typ.Func().Param("self", typ.Self).Returns(cdcChannelType).Build()},
		{Name: "receive", Type: typ.Func().Param("self", typ.Self).Returns(cdcChannelType).Build()},
		{Name: "close", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "release", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	})
}

func ModuleTypes() *io.Manifest {
	m := io.NewManifest("cdc")
	m.DefineType("Capabilities", sourceCapabilitiesType)
	m.DefineType("SourceInfo", sourceInfoType)
	m.DefineType("StreamOptions", streamOptionsType)
	m.DefineType("Change", changeType)

	moduleType := typ.NewInterface("cdc", []typ.Method{
		{Name: "list_sources", Type: typ.Func().Returns(typ.NewArray(sourceInfoType), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "source", Type: typ.Func().Param("name", typ.String).Returns(typ.NewOptional(sourceInfoType), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "stream", Type: typ.Func().Param("name", typ.String).OptParam("opts", streamOptionsType).Returns(cdcStreamType, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
