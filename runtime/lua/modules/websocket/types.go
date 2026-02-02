package websocket

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

// Message type received from websocket channel
var messageType = typ.NewRecord().
	Field("type", typ.String).
	Field("data", typ.NewOptional(typ.String)).
	Build()

// Forward declarations for init-time resolution
var (
	messageChannelType typ.Type
	clientType         *typ.Interface
)

func init() {
	// Resolve Channel<Message> type
	messageChannelType = typ.Any
	if manifest := engine.ChannelModuleTypes(); manifest != nil {
		if t, ok := manifest.LookupType("Channel"); ok {
			if gen, ok := t.(*typ.Generic); ok {
				messageChannelType = typ.Instantiate(gen, messageType)
			}
		}
	}

	clientType = typ.NewInterface("websocket.Client", []typ.Method{
		{Name: "send", Type: typ.Func().Param("self", typ.Self).Param("message", typ.String).OptParam("type", typ.Number).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "receive", Type: typ.Func().Param("self", typ.Self).Returns(messageType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "channel", Type: typ.Func().Param("self", typ.Self).Returns(messageChannelType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "close", Type: typ.Func().Param("self", typ.Self).OptParam("code", typ.Number).OptParam("message", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "ping", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	})
}

// COMPRESSION constants as a record
var compressionConstType = typ.NewRecord().
	Field("DISABLED", typ.Number).
	Field("CONTEXT_TAKEOVER", typ.Number).
	Field("NO_CONTEXT", typ.Number).
	Build()

// CLOSE_CODES constants as a record
var closeCodesConstType = typ.NewRecord().
	Field("NORMAL", typ.Number).
	Field("GOING_AWAY", typ.Number).
	Field("PROTOCOL_ERROR", typ.Number).
	Field("UNSUPPORTED_DATA", typ.Number).
	Field("RESERVED", typ.Number).
	Field("NO_STATUS", typ.Number).
	Field("ABNORMAL_CLOSURE", typ.Number).
	Field("INVALID_PAYLOAD", typ.Number).
	Field("POLICY_VIOLATION", typ.Number).
	Field("MESSAGE_TOO_BIG", typ.Number).
	Field("MANDATORY_EXTENSION", typ.Number).
	Field("INTERNAL_ERROR", typ.Number).
	Field("SERVICE_RESTART", typ.Number).
	Field("TRY_AGAIN_LATER", typ.Number).
	Field("BAD_GATEWAY", typ.Number).
	Field("TLS_HANDSHAKE", typ.Number).
	Build()

// ModuleTypes returns the type manifest for the websocket module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("websocket")

	m.DefineType("Client", clientType)
	m.DefineType("Message", messageType)
	m.DefineType("COMPRESSION", compressionConstType)
	m.DefineType("CLOSE_CODES", closeCodesConstType)

	// Methods interface
	moduleMethods := typ.NewInterface("websocket", []typ.Method{
		{Name: "connect", Type: typ.Func().Param("url", typ.String).OptParam("options", typ.Any).Returns(clientType, typ.NewOptional(typ.LuaError)).Build()},
	})

	// Constant fields
	moduleFields := typ.NewRecord().
		Field("TYPE_TEXT", typ.String).
		Field("TYPE_BINARY", typ.String).
		Field("TYPE_PING", typ.String).
		Field("TYPE_PONG", typ.String).
		Field("TYPE_CLOSE", typ.String).
		Field("TEXT", typ.Number).
		Field("BINARY", typ.Number).
		Field("COMPRESSION", compressionConstType).
		Field("CLOSE_CODES", closeCodesConstType).
		Build()

	m.SetExport(typ.NewIntersection(moduleMethods, moduleFields))
	return m
}
