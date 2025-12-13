package process

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
	lua "github.com/yuin/gopher-lua"
)

// context is used in MessageHandler signature

const messageTypeName = "process.Message"

type Message struct {
	Topic    string
	Payloads payload.Payloads
	From     pid.PID
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

	if len(msg.Payloads) == 0 {
		l.Push(lua.LNil)
		return 1
	}

	if len(msg.Payloads) == 1 {
		return payloadmod.PushPayload(l, msg.Payloads[0])
	}

	tbl := l.CreateTable(len(msg.Payloads), 0)
	for i, pl := range msg.Payloads {
		tbl.RawSetInt(i+1, payloadmod.WrapPayload(l, pl))
	}
	l.Push(tbl)
	return 1
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

func NewMessage(from pid.PID, topic string, payloads payload.Payloads) *Message {
	return &Message{
		Topic:    topic,
		Payloads: payloads,
		From:     from,
	}
}

func WrapMessage(l *lua.LState, m *Message) lua.LValue {
	return value.NewTypedUserData(l, m, messageTypeName)
}

func messageToString(l *lua.LState) int {
	msg := checkMessage(l)
	if msg == nil {
		return 0
	}
	l.Push(lua.LString("process.Message{topic=" + msg.Topic + "}"))
	return 1
}

// MessageHandler creates messages from incoming payloads for channel delivery.
func MessageHandler(_ context.Context, l *lua.LState, source pid.PID, topic string, payloads []payload.Payload) lua.LValue {
	msg := NewMessage(source, topic, payloads)
	return WrapMessage(l, msg)
}
