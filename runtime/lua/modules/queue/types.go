// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// Message type
var messageType = typ.NewInterface("queue.Message", []typ.Method{
	{
		Name: "id",
		Type: typ.Func().Param("_", typ.Self).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
	},
	{
		Name: "header",
		Type: typ.Func().Param("_", typ.Self).Param("key", typ.String).Returns(typ.NewOptional(typ.Any), typ.NewOptional(typ.LuaError)).Build(),
	},
	{
		Name: "headers",
		Type: typ.Func().Param("_", typ.Self).Returns(typ.NewMap(typ.String, typ.Any), typ.NewOptional(typ.LuaError)).Build(),
	},
	{
		Name: "ack",
		Type: typ.Func().Param("_", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build(),
	},
	{
		Name: "nack",
		Type: typ.Func().Param("_", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build(),
	},
})

// ModuleTypes returns the type manifest for the queue module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("queue")

	m.DefineType("Message", messageType)

	moduleType := typ.NewInterface("queue", []typ.Method{
		{
			Name: "publish",
			Type: typ.Func().Param("topic", typ.String).Param("message", typ.Any).OptParam("options", typ.Any).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "message",
			Type: typ.Func().Returns(messageType, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "info",
			Type: typ.Func().Param("id", typ.String).Returns(typ.NewMap(typ.String, typ.Any), typ.NewOptional(typ.LuaError)).Build(),
		},
	})

	m.SetExport(moduleType)
	return m
}
