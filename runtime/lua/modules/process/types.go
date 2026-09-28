// SPDX-License-Identifier: MPL-2.0

package process

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

var (
	messageType        typ.Type
	processEventType   typ.Type
	messageChannelType typ.Type
	eventChannelType   typ.Type
	rawChannelType     typ.Type
	channelGen         *typ.Generic
)

var messageElement = typ.NewTypeParam("T", nil)
var messageGeneric = typ.NewGeneric("process.Message", []*typ.TypeParam{messageElement},
	typ.NewInterface("process.Message", []typ.Method{
		{Name: "from", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "topic", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "payload", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any).Build()},
		{Name: "data", Type: typ.Func().Param("self", typ.Self).Returns(messageElement).Build()},
	}))

// The type argument belongs to the receiving process. Message mode changes the
// envelope only; the same T is checked for both raw and message delivery.
func listenType() typ.Type {
	t := typ.NewTypeParam("T", nil)
	rawOptions := typ.NewRecord().OptField("message", typ.False).Build()
	messageOptions := typ.NewRecord().Field("message", typ.True).Build()
	typedRawOptions := typ.NewRecord().OptField("message", typ.False).Field("type", typ.NewMeta(t)).Build()
	typedMessageOptions := typ.NewRecord().Field("message", typ.True).Field("type", typ.NewMeta(t)).Build()
	dynamicOptions := typ.NewRecord().OptField("message", typ.Boolean).Build()
	typedDynamicOptions := typ.NewRecord().OptField("message", typ.Boolean).Field("type", typ.NewMeta(t)).Build()
	return typ.NewUnion(
		typ.Func().TypeParam("T", nil).Param("topic", typ.String).Param("options", typedDynamicOptions).
			Returns(typ.NewUnion(typ.Instantiate(channelGen, t), typ.Instantiate(channelGen, typ.Instantiate(messageGeneric, t))), typ.NewOptional(typ.LuaError)).Build(),
		typ.Func().Param("topic", typ.String).Param("options", dynamicOptions).
			Returns(typ.NewUnion(rawChannelType, messageChannelType), typ.NewOptional(typ.LuaError)).Build(),
		typ.Func().TypeParam("T", nil).Param("topic", typ.String).Param("options", typedMessageOptions).
			Returns(typ.Instantiate(channelGen, typ.Instantiate(messageGeneric, t)), typ.NewOptional(typ.LuaError)).Build(),
		typ.Func().TypeParam("T", nil).Param("topic", typ.String).Param("options", typedRawOptions).
			Returns(typ.Instantiate(channelGen, t), typ.NewOptional(typ.LuaError)).Build(),
		typ.Func().Param("topic", typ.String).Param("options", messageOptions).
			Returns(messageChannelType, typ.NewOptional(typ.LuaError)).Build(),
		typ.Func().Param("topic", typ.String).OptParam("options", rawOptions).
			Returns(rawChannelType, typ.NewOptional(typ.LuaError)).Build(),
	)
}

func init() {
	messageType = typ.Instantiate(messageGeneric, typ.Any)

	eventRecord := typ.NewRecord().
		Field("kind", typ.String).
		Field("from", typ.String).
		OptField("result", typ.Any).
		OptField("error", typ.Any).
		OptField("reason", typ.String).
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
	Field("upgradable", typ.Boolean).
	Build()

// set_options applies a partial update. get_options always returns both fields,
// but callers may set either option independently (or pass an empty table).
var processOptionsUpdateType = typ.NewRecord().
	OptField("trap_links", typ.Boolean).
	OptField("upgradable", typ.Boolean).
	Build()

var eventType = typ.NewRecord().
	Field("CANCEL", typ.String).
	Field("EXIT", typ.String).
	Field("LINK_DOWN", typ.String).
	Field("OUTDATED", typ.String).
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
		{Name: "exec", Type: typ.Func().
			Param("self", typ.Self).
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.Any, typ.NewOptional(typ.LuaError)).
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
		Field("listen", listenType()).
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
			Param("opts", processOptionsUpdateType).
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
