// SPDX-License-Identifier: MPL-2.0

package process

import (
	"github.com/wippyai/go-lua/types/constraint"
	"github.com/wippyai/go-lua/types/contract"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

var (
	messageType        *typ.Interface
	processEventType   typ.Type
	messageChannelType typ.Type
	eventChannelType   typ.Type
	rawChannelType     typ.Type
	channelGen         *typ.Generic
)

func init() {
	messageType = typ.NewInterface("process.Message", []typ.Method{
		{Name: "from", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "topic", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "payload", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any).Build()},
	})

	eventRecord := typ.NewRecord().
		Field("kind", typ.String).
		Field("from", typ.String).
		OptField("result", typ.Any).
		OptField("error", typ.Any).
		OptField("deadline", typ.String).
		Build()
	eventMethods := typ.NewInterface("process.EventMethods", []typ.Method{
		{Name: "payload", Type: typ.Func().
			Param("self", typ.Self).
			Returns(typ.NewOptional(typ.Any)).
			Build()},
	})
	processEventType = typ.NewAlias("process.Event", typ.NewIntersection(eventRecord, eventMethods))

	if manifest := engine.ChannelModuleTypes(); manifest != nil {
		if t, ok := manifest.LookupType("Channel"); ok {
			if gen, ok := t.(*typ.Generic); ok {
				channelGen = gen
				messageChannelType = typ.Instantiate(channelGen, messageType)
				eventChannelType = typ.Instantiate(channelGen, processEventType)
				rawChannelType = typ.Instantiate(channelGen, typ.Any)
			}
		}
	}
	if messageChannelType == nil {
		messageChannelType = typ.Any
	}
	if eventChannelType == nil {
		eventChannelType = typ.Any
	}
	if rawChannelType == nil {
		rawChannelType = typ.Any
	}
}

var processOptionsType = typ.NewRecord().
	Field("trap_links", typ.Boolean).
	Build()

var eventType = typ.NewRecord().
	Field("CANCEL", typ.String).
	Field("EXIT", typ.String).
	Field("LINK_DOWN", typ.String).
	Build()

// process.registry surface: scoped registration with optional foreign PID.
// Scope constants live on the same table as the methods (LOCAL, EVENTUAL,
// CONSISTENT, STRONG), exposed as numeric tags.
var registryFieldsType = typ.NewRecord().
	Field("LOCAL", typ.Number).
	Field("EVENTUAL", typ.Number).
	Field("CONSISTENT", typ.Number).
	Field("STRONG", typ.Number).
	Build()

var registryMethodsType = typ.NewInterface("process.registry", []typ.Method{
	{Name: "register", Type: typ.Func().
		Param("name", typ.String).
		OptParam("pid", typ.String).
		OptParam("scope", typ.Number).
		Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
		Build()},
	{Name: "lookup", Type: typ.Func().
		Param("name", typ.String).
		Returns(typ.String, typ.NewOptional(typ.LuaError)).
		Build()},
	{Name: "unregister", Type: typ.Func().
		Param("name", typ.String).
		OptParam("scope", typ.Number).
		Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
		Build()},
})

var registrySubType = typ.NewIntersection(registryMethodsType, registryFieldsType)

var spawnBuilderType *typ.Interface

func init() {
	spawnBuilderType = typ.NewInterface("process.SpawnBuilder", []typ.Method{
		{Name: "with_context", Type: typ.Func().
			Param("self", typ.Self).
			Param("context", typ.NewMap(typ.String, typ.Any)).
			Returns(typ.Self).
			Build()},
		{Name: "with_options", Type: typ.Func().
			Param("self", typ.Self).
			Param("options", typ.NewMap(typ.String, typ.Any)).
			Returns(typ.Self).
			Build()},
		{Name: "with_actor", Type: typ.Func().
			Param("self", typ.Self).
			Param("actor", typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "with_scope", Type: typ.Func().
			Param("self", typ.Self).
			Param("scope", typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "with_name", Type: typ.Func().
			Param("self", typ.Self).
			Param("name", typ.String).
			Returns(typ.Self).
			Build()},
		{Name: "with_message", Type: typ.Func().
			Param("self", typ.Self).
			Param("msg", typ.String).
			Variadic(typ.Any).
			Returns(typ.Self).
			Build()},
		{Name: "spawn", Type: typ.Func().
			Param("self", typ.Self).
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "spawn_monitored", Type: typ.Func().
			Param("self", typ.Self).
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "spawn_linked", Type: typ.Func().
			Param("self", typ.Self).
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "spawn_linked_monitored", Type: typ.Func().
			Param("self", typ.Self).
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
	})
}

func ModuleTypes() *io.Manifest {
	m := io.NewManifest("process")

	m.DefineType("Message", messageType)
	m.DefineType("Event", processEventType)
	m.DefineType("Options", processOptionsType)
	m.DefineType("SpawnBuilder", spawnBuilderType)

	moduleFieldsType := typ.NewRecord().
		Field("event", eventType).
		Field("registry", registrySubType).
		Build()

	moduleMethodsType := typ.NewInterface("process", []typ.Method{
		{Name: "id", Type: typ.Func().Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "pid", Type: typ.Func().Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "send", Type: typ.Func().
			Param("pid", typ.String).
			Param("topic", typ.String).
			Variadic(typ.Any).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "spawn", Type: typ.Func().
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "spawn_monitored", Type: typ.Func().
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "spawn_linked", Type: typ.Func().
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "spawn_linked_monitored", Type: typ.Func().
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "terminate", Type: typ.Func().
			Param("pid", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "cancel", Type: typ.Func().
			Param("pid", typ.String).
			OptParam("reason", typ.Any).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "get_options", Type: typ.Func().
			Returns(processOptionsType).
			Build()},
		{Name: "set_options", Type: typ.Func().
			Param("opts", processOptionsType).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "monitor", Type: typ.Func().
			Param("pid", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "unmonitor", Type: typ.Func().
			Param("pid", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "link", Type: typ.Func().
			Param("pid", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "unlink", Type: typ.Func().
			Param("pid", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "with_context", Type: typ.Func().
			Param("context", typ.NewMap(typ.String, typ.Any)).
			Returns(spawnBuilderType).
			Build()},
		{Name: "with_options", Type: typ.Func().
			Param("options", typ.NewMap(typ.String, typ.Any)).
			Returns(spawnBuilderType).
			Build()},
		{Name: "inbox", Type: typ.Func().
			Returns(messageChannelType).
			Build()},
		{Name: "events", Type: typ.Func().
			Returns(eventChannelType).
			Build()},
		{Name: "listen", Type: typ.Func().
			Param("topic", typ.String).
			OptParam("options", typ.Any).
			Returns(rawChannelType, typ.NewOptional(typ.LuaError)).
			Spec(contract.NewSpec().WithReturnCase(
				constraint.FromConstraints(constraint.FieldEquals{
					Target: constraint.ParamPath(1),
					Field:  "message",
					Value:  typ.True,
				}),
				messageChannelType,
			)).
			Build()},
		{Name: "unlisten", Type: typ.Func().
			Param("listener", typ.Any).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "upgrade", Type: typ.Func().
			OptParam("path", typ.String).
			Variadic(typ.Any).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "exec", Type: typ.Func().
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.Any, typ.NewOptional(typ.LuaError)).
			Build()},
	})

	m.SetExport(typ.NewIntersection(moduleMethodsType, moduleFieldsType))
	return m
}
