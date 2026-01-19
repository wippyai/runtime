package process

import (
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/yuin/gopher-lua/types/contract"
	"github.com/yuin/gopher-lua/types/io"
	"github.com/yuin/gopher-lua/types/predicate"
	"github.com/yuin/gopher-lua/types/typ"
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

var registrySubType = typ.NewInterface("process.registry", []typ.Method{
	{Name: "register", Type: typ.Func().
		Param("s", typ.String).
		OptParam("n", typ.String).
		Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
		Build()},
	{Name: "lookup", Type: typ.Func().
		Param("s", typ.String).
		Returns(typ.String, typ.NewOptional(typ.LuaError)).
		Build()},
	{Name: "unregister", Type: typ.Func().
		Param("s", typ.String).
		Returns(typ.Boolean).
		Build()},
})

var spawnBuilderType *typ.Interface

func init() {
	spawnBuilderType = typ.NewInterface("process.SpawnBuilder", []typ.Method{
		{Name: "with_context", Type: typ.Func().
			Param("self", typ.Self).
			Param("context", typ.NewMap(typ.String, typ.Any)).
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
		{Name: "id", Type: typ.Func().Returns(typ.String, typ.NewOptional(typ.String)).Build()},
		{Name: "pid", Type: typ.Func().Returns(typ.String).Build()},
		{Name: "send", Type: typ.Func().
			Param("pid", typ.String).
			Param("topic", typ.String).
			Variadic(typ.Any).
			Returns(typ.Boolean, typ.NewOptional(typ.String)).
			Build()},
		{Name: "spawn", Type: typ.Func().
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.String, typ.NewOptional(typ.String)).
			Build()},
		{Name: "spawn_monitored", Type: typ.Func().
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.String, typ.NewOptional(typ.String)).
			Build()},
		{Name: "spawn_linked", Type: typ.Func().
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.String, typ.NewOptional(typ.String)).
			Build()},
		{Name: "spawn_linked_monitored", Type: typ.Func().
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.String, typ.NewOptional(typ.String)).
			Build()},
		{Name: "terminate", Type: typ.Func().
			Param("pid", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.String)).
			Build()},
		{Name: "cancel", Type: typ.Func().
			Param("pid", typ.String).
			OptParam("reason", typ.Any).
			Returns(typ.Boolean, typ.NewOptional(typ.String)).
			Build()},
		{Name: "get_options", Type: typ.Func().
			Returns(processOptionsType).
			Build()},
		{Name: "set_options", Type: typ.Func().
			Param("opts", processOptionsType).
			Returns(typ.Boolean, typ.NewOptional(typ.String)).
			Build()},
		{Name: "monitor", Type: typ.Func().
			Param("pid", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.String)).
			Build()},
		{Name: "unmonitor", Type: typ.Func().
			Param("pid", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.String)).
			Build()},
		{Name: "link", Type: typ.Func().
			Param("pid", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.String)).
			Build()},
		{Name: "unlink", Type: typ.Func().
			Param("pid", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.String)).
			Build()},
		{Name: "with_context", Type: typ.Func().
			Param("context", typ.NewMap(typ.String, typ.Any)).
			Returns(spawnBuilderType).
			Build()},
		{Name: "inbox", Type: typ.Func().
			Returns(messageChannelType, typ.NewOptional(typ.String)).
			Build()},
		{Name: "events", Type: typ.Func().
			Returns(eventChannelType, typ.NewOptional(typ.String)).
			Build()},
		{Name: "listen", Type: typ.Func().
			Param("topic", typ.String).
			OptParam("options", typ.Any).
			Returns(rawChannelType, typ.NewOptional(typ.String)).
			Spec(contract.NewSpec().WithReturnCase(
				predicate.FieldEquals{
					Target: "param[1]",
					Field:  "message",
					Value:  predicate.BoolLiteral(true),
				},
				messageChannelType,
			)).
			Build()},
		{Name: "unlisten", Type: typ.Func().
			Param("listener", typ.Any).
			Returns(typ.Boolean, typ.NewOptional(typ.String)).
			Build()},
		{Name: "upgrade", Type: typ.Func().
			OptParam("path", typ.String).
			Variadic(typ.Any).
			Returns(typ.Boolean, typ.NewOptional(typ.String)).
			Build()},
		{Name: "run", Type: typ.Func().
			Param("module", typ.String).
			Param("func", typ.String).
			Variadic(typ.Any).
			Returns(typ.Any, typ.NewOptional(typ.String)).
			Build()},
	})

	m.SetExport(typ.NewIntersection(moduleMethodsType, moduleFieldsType))
	return m
}
