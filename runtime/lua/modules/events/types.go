// SPDX-License-Identifier: MPL-2.0

package events

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ModuleTypes returns the type manifest for the events module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("events")

	moduleType := typ.NewInterface("events", []typ.Method{
		{Name: "subscribe", Type: typ.Func().Param("topic", typ.String).OptParam("group", typ.String).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "send", Type: typ.Func().Param("topic", typ.String).Param("source", typ.String).Param("event_type", typ.String).OptParam("payload", typ.Any).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
