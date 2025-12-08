package process

import (
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
	lua "github.com/yuin/gopher-lua"
)

const messageTypeName = "process.Message"

type Message struct {
	Topic   string
	Payload payload.Payload
	From    relay.PID
}

var messageMethods = map[string]lua.LGoFunc{
	"topic":   messageTopic,
	"payload": messagePayload,
	"from":    messageFrom,
}

func messageTopic(l *lua.LState) int {
	msg := checkMessage(l)
	if msg == nil {
		return 0
	}
	l.Push(lua.LString(msg.Topic))
	return 1
}

func messagePayload(l *lua.LState) int {
	msg := checkMessage(l)
	if msg == nil {
		return 0
	}
	return payloadmod.PushPayload(l, msg.Payload)
}

func messageFrom(l *lua.LState) int {
	msg := checkMessage(l)
	if msg == nil {
		return 0
	}

	if msg.From.String() == "{||}" {
		l.Push(lua.LNil)
		return 1
	}

	l.Push(lua.LString(msg.From.String()))
	return 1
}

func checkMessage(l *lua.LState) *Message {
	ud := l.CheckUserData(1)
	if msg, ok := ud.Value.(*Message); ok {
		return msg
	}
	l.ArgError(1, "message expected")
	return nil
}

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
	ud.Metatable = messageMetatable
	return ud
}

func messageToString(l *lua.LState) int {
	msg := checkMessage(l)
	if msg == nil {
		return 0
	}
	l.Push(lua.LString("process.Message{topic=" + msg.Topic + "}"))
	return 1
}
