package queue

import (
	"sync"

	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// Module represents the queue Lua module
type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

// NewModule creates a new queue module instance
func NewModule() *Module {
	return &Module{}
}

func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "queue",
		Description: "Message queue operations",
		Class:       []string{luaapi.ClassIO},
	}
}

// Loader registers the module's functions into Lua state
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	// Register message type for each Lua state
	m.registerMessageType(l)

	l.Push(m.moduleTable)
	return 1
}

// initModuleTable creates and initializes the module table once
func (m *Module) initModuleTable(l *lua.LState) {
	// Create module table with exact pre-allocated size
	t := l.CreateTable(0, 2) // 2 functions: publish, message

	// Register functions
	t.RawSetString("publish", l.NewFunction(m.publish))
	t.RawSetString("message", l.NewFunction(m.message))

	// Make the table immutable
	t.Immutable = true

	m.moduleTable = t
}

// registerMessageType registers the Message userdata type and its methods
func (m *Module) registerMessageType(l *lua.LState) {
	value.RegisterTypeMethods(l, "QueueMessage",
		map[string]lua.LGFunction{
			"__tostring": messageToString,
		},
		map[string]lua.LGFunction{
			"id":      messageID,
			"header":  messageHeader,
			"headers": messageHeaders,
		},
	)
}

// publish publishes a message to a queue
func (m *Module) publish(l *lua.LState) int {
	// Get context
	ctx := l.Context()

	// Get queue manager from context
	queueMgr := queueapi.GetManager(ctx)
	if queueMgr == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("queue manager not found in context"))
		return 2
	}

	// Get queue ID (first argument)
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("queue ID required"))
		return 2
	}

	queueIDStr := l.CheckString(1)
	queueID := registry.ParseID(queueIDStr)

	// Get message data (second argument)
	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("message data required"))
		return 2
	}

	data := l.CheckAny(2)

	// Convert Lua value to payload using proper externalization
	p := luaconv.ExportPayload(data)

	// Create message
	msg := queueapi.NewMessage(p)

	// Process optional headers (third argument)
	if l.GetTop() >= 3 {
		headersArg := l.Get(3)
		if tbl, ok := headersArg.(*lua.LTable); ok {
			tbl.ForEach(func(key, val lua.LValue) {
				keyStr, ok := key.(lua.LString)
				if !ok {
					return
				}
				msg.Headers.Set(string(keyStr), value.ToGoAny(val))
			})
		}
	}

	// Publish message
	err := queueMgr.Publish(ctx, queueID, msg)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// message retrieves the current message from the delivery context
func (m *Module) message(l *lua.LState) int {
	// Get context
	ctx := l.Context()

	// Get delivery from context
	delivery, ok := queueapi.GetDelivery(ctx)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("no delivery found in context"))
		return 2
	}

	// Create message userdata
	ud := l.NewUserData()
	ud.Value = &Message{message: delivery.Message}
	ud.Metatable = value.GetTypeMetatable(l, "QueueMessage")

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}
