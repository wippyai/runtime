package queue

import (
	"fmt"
	"sync"

	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const messageTypeName = "queue.Message"

var (
	moduleTable      *lua.LTable
	registration     *lua2api.Registration
	messageMetatable *lua.LTable
	initOnce         sync.Once
)

// Module is the singleton queue module instance.
var Module = &queueModule{}

type queueModule struct{}

func (m *queueModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "queue",
		Description: "Message queue operations",
		Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	}
}

func (m *queueModule) Register(l *lua.LState) *lua2api.Registration {
	initOnce.Do(func() {
		moduleTable = createModuleTable()

		messageMetatable = value.RegisterTypeMethods(nil, messageTypeName,
			map[string]lua.LGFunction{"__tostring": messageToString},
			messageMethods)

		registration = &lua2api.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	return registration
}

func (m *queueModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use lua2api.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	lua2api.LoadModule(l, Module)
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 2)
	mod.RawSetString("publish", lua.LGoFunc(publish))
	mod.RawSetString("message", lua.LGoFunc(message))
	mod.Immutable = true
	return mod
}

type Message struct {
	message *queueapi.Message
}

var messageMethods = map[string]lua.LGFunction{
	"id":      messageID,
	"header":  messageHeader,
	"headers": messageHeaders,
}

func checkMessage(l *lua.LState, idx int) *Message {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Message); ok {
		return v
	}
	l.ArgError(idx, "queue.Message expected")
	return nil
}

func publish(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	queueMgr := queueapi.GetManager(ctx)
	if queueMgr == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("queue manager not found in context"))
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("queue ID required"))
		return 2
	}

	queueIDStr := l.CheckString(1)
	queueID := registry.ParseID(queueIDStr)

	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("message data required"))
		return 2
	}

	data := l.CheckAny(2)
	p := luaconv.ExportPayload(data)
	msg := queueapi.NewMessage(p)

	if l.GetTop() >= 3 {
		headersArg := l.Get(3)
		if tbl, ok := headersArg.(*lua.LTable); ok {
			tbl.ForEach(func(key, val lua.LValue) {
				keyStr, ok := key.(lua.LString)
				if !ok {
					return
				}
				msg.Headers.Set(string(keyStr), toGoValue(val))
			})
		}
	}

	yield := AcquirePublishYield()
	yield.Manager = queueMgr
	yield.QueueID = queueID
	yield.Message = msg
	l.Push(yield)
	return -1
}

func message(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	delivery, ok := queueapi.GetDelivery(ctx)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("no delivery found in context"))
		return 2
	}

	ud := l.NewUserData()
	ud.Value = &Message{message: delivery.Message}
	ud.Metatable = messageMetatable

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

func messageID(l *lua.LState) int {
	msg := checkMessage(l, 1)
	if msg == nil {
		return 0
	}
	l.Push(lua.LString(msg.message.ID))
	l.Push(lua.LNil)
	return 2
}

func messageHeader(l *lua.LState) int {
	msg := checkMessage(l, 1)
	if msg == nil {
		return 0
	}

	key := l.CheckString(2)
	if msg.message.Headers == nil {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	val, ok := msg.message.Headers.Get(key)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	l.Push(toLuaValue(l, val))
	l.Push(lua.LNil)
	return 2
}

func messageHeaders(l *lua.LState) int {
	msg := checkMessage(l, 1)
	if msg == nil {
		return 0
	}
	tbl := lua.CreateTable(0, 10)
	if msg.message.Headers != nil {
		for key, value := range msg.message.Headers { // todo: collisiton
			tbl.RawSetString(key, toLuaValue(l, value))
		}
	}
	l.Push(tbl)
	l.Push(lua.LNil)
	return 2
}

func messageToString(l *lua.LState) int {
	msg := checkMessage(l, 1)
	if msg == nil {
		return 0
	}
	l.Push(lua.LString(fmt.Sprintf("queue.Message{id=%s}", msg.message.ID)))
	return 1
}

func toGoValue(v lua.LValue) any {
	switch v := v.(type) {
	case lua.LBool:
		return bool(v)
	case lua.LNumber:
		return float64(v)
	case lua.LInteger:
		return int64(v)
	case lua.LString:
		return string(v)
	case *lua.LNilType:
		return nil
	default:
		return nil
	}
}

func toLuaValue(l *lua.LState, val any) lua.LValue {
	switch v := val.(type) {
	case string:
		return lua.LString(v)
	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)
	case float64:
		return lua.LNumber(v)
	case bool:
		return lua.LBool(v)
	default:
		return lua.LString(fmt.Sprintf("%v", v))
	}
}
