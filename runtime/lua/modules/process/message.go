package process

import (
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
	lua "github.com/yuin/gopher-lua"
)

const (
	// MessageTypeName is the type name for the message userdata in Lua
	MessageTypeName = "message"
)

// Message represents a message with topic and payload
type Message struct {
	Topic   string
	Payload payload.Payload
	From    relay.PID // Added From field to track the sender
}

// RegisterMessageType registers the message type with Lua
func RegisterMessageType(l *lua.LState) {
	value.RegisterTypeMethods(l, MessageTypeName, nil, map[string]lua.LGFunction{
		"topic":   messageTopic,
		"payload": messagePayload,
		"from":    messageFrom, // Added from method
	})
}

// messageTopic returns the topic of a message
// Method: message:topic()
// Returns: topic string
func messageTopic(l *lua.LState) int {
	msg := CheckMessage(l)
	l.Push(lua.LString(msg.Topic))
	return 1
}

// messagePayload returns the payload of a message as userdata
// Method: message:payload()
// Returns: payload userdata
func messagePayload(l *lua.LState) int {
	msg := CheckMessage(l)
	return payloadmod.PushPayload(l, msg.Payload)
}

// messageFrom returns the sender of a message
// Method: message:from()
// Returns: PID string or nil if not available
func messageFrom(l *lua.LState) int {
	msg := CheckMessage(l)

	// Check if From field is empty
	if msg.From.String() == "{||}" {
		l.Push(lua.LNil) // Return nil if sender PID is empty
		return 1
	}

	// Push the PID string to the Lua stack
	l.Push(lua.LString(msg.From.String()))
	return 1
}

// CheckMessage gets a message from the Lua stack
// Returns the Message object or raises an error
func CheckMessage(l *lua.LState) *Message {
	ud := l.CheckUserData(1)
	if msg, ok := ud.Value.(*Message); ok {
		return msg
	}
	l.ArgError(1, "message expected")
	return nil
}

// NewMessage creates a new Message object with sender information
func NewMessage(from relay.PID, topic string, p payload.Payload) *Message {
	return &Message{
		Topic:   topic,
		Payload: p,
		From:    from,
	}
}

func WrapMessage(l *lua.LState, m *Message) lua.LValue {
	ud := l.NewUserData()
	ud.Value = m
	ud.Metatable = value.GetTypeMetatable(l, MessageTypeName)
	return ud
}
