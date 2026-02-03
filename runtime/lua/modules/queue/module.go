package queue

import (
	"fmt"

	lua "github.com/wippyai/go-lua"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
)

const messageTypeName = "queue.Message"

func init() {
	value.RegisterTypeMethods(nil, messageTypeName,
		map[string]lua.LGoFunc{"__tostring": messageToString},
		messageMethods)
}

// Module is the queue module definition.
var Module = &luaapi.ModuleDef{
	Name:        "queue",
	Description: "Message queue operations",
	Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 2)
	mod.RawSetString("publish", lua.LGoFunc(publish))
	mod.RawSetString("message", lua.LGoFunc(message))
	mod.Immutable = true
	return mod, nil
}

type Message struct {
	message *queueapi.Message
}

var messageMethods = map[string]lua.LGoFunc{
	"id":      messageID,
	"header":  messageHeader,
	"headers": messageHeaders,
}

func checkMessage(l *lua.LState, _ int) *Message {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Message); ok {
		return v
	}
	l.ArgError(1, "queue.Message expected")
	return nil
}

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func publish(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return invalidError(l, "no context")
	}

	// General permission check
	if !security.IsAllowed(ctx, "queue.publish", "", nil) {
		return invalidError(l, "queue publishing not allowed")
	}

	queueMgr := queueapi.GetManager(ctx)
	if queueMgr == nil {
		return invalidError(l, "queue manager not found in context")
	}

	if l.GetTop() < 1 {
		return invalidError(l, "queue ID required")
	}

	queueIDStr := l.CheckString(1)
	if queueIDStr == "" {
		return invalidError(l, "queue ID required")
	}

	// Queue-specific permission check
	if !security.IsAllowed(ctx, "queue.publish.queue", queueIDStr, nil) {
		return invalidError(l, "not allowed to publish to queue: "+queueIDStr)
	}

	queueID := registry.ParseID(queueIDStr)

	if l.GetTop() < 2 {
		return invalidError(l, "message data required")
	}

	data := l.CheckAny(2)
	if tbl, ok := data.(*lua.LTable); ok && tbl.Len() == 0 && tbl.RawGetString("") == lua.LNil {
		// Check if table is completely empty (no array or map entries)
		empty := true
		tbl.ForEach(func(_, _ lua.LValue) { empty = false })
		if empty {
			return invalidError(l, "message data cannot be empty")
		}
	}
	p := luaconv.ExportPayload(data)
	msg := queueapi.NewMessage(p)

	// Process optional headers
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

	// Publish directly via manager
	if err := queueMgr.Publish(ctx, queueID, msg); err != nil {
		return internalError(l, err, "publish failed")
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func message(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return invalidError(l, "no context")
	}

	delivery, ok := queueapi.GetDelivery(ctx)
	if !ok {
		return invalidError(l, "no delivery found in context")
	}

	value.PushTypedUserData(l, &Message{message: delivery.Message}, messageTypeName)
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

	l.Push(toLuaValue(val))
	l.Push(lua.LNil)
	return 2
}

func messageHeaders(l *lua.LState) int {
	msg := checkMessage(l, 1)
	if msg == nil {
		return 0
	}

	tbl := lua.CreateTable(0, len(msg.message.Headers))
	if msg.message.Headers != nil {
		for key, val := range msg.message.Headers {
			tbl.RawSetString(key, toLuaValue(val))
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

func toLuaValue(val any) lua.LValue {
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
