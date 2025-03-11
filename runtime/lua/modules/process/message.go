package process

import (
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	payloadmod "github.com/ponyruntime/pony/runtime/lua/modules/payload"
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
}

// RegisterMessageType registers the message type with Lua
func RegisterMessageType(l *lua.LState) {
	value.RegisterTypeMethods(l, MessageTypeName, nil, map[string]lua.LGFunction{
		"topic":   messageTopic,
		"payload": messagePayload,
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

// PushMessage creates a message userdata and pushes it onto the stack
// Returns 1 (number of values pushed)
func PushMessage(l *lua.LState, topic string, p payload.Payload) int {
	ud := l.NewUserData()
	ud.Value = &Message{Topic: topic, Payload: p}
	ud.Metatable = value.GetTypeMetatable(l, MessageTypeName)
	l.Push(ud)
	return 1
}

// NewMessage creates a new Message object
func NewMessage(topic string, p payload.Payload) *Message {
	return &Message{
		Topic:   topic,
		Payload: p,
	}
}

func WrapMessage(l *lua.LState, m *Message) lua.LValue {
	ud := l.NewUserData()
	ud.Value = m
	ud.Metatable = value.GetTypeMetatable(l, MessageTypeName)
	return ud
}
