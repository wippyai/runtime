package websocket

import (
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module represents the WebSocket module
type Module struct {
	log *zap.Logger
}

// NewWebSocketModule creates a new WebSocket module
func NewWebSocketModule(log *zap.Logger) *Module {
	return &Module{log: log}
}

// Name returns the module name
func (m *Module) Name() string {
	return "websocket"
}

// Loader registers the WebSocket module
// Loader registers the WebSocket module
func (m *Module) Loader(l *lua.LState) int {
	// Create table with pre-allocated size for our known elements
	mod := l.CreateTable(0, 7) // 1 function + 5 types + 1 close codes table

	// Register connection function using RawSetString
	mod.RawSetString("connect", l.NewFunction(wsConnect))

	// Register message types using RawSetString
	mod.RawSetString("TYPE_TEXT", lua.LString(TypeText))
	mod.RawSetString("TYPE_BINARY", lua.LString(TypeBinary))
	mod.RawSetString("TYPE_PING", lua.LString(TypePing))
	mod.RawSetString("TYPE_PONG", lua.LString(TypePong))
	mod.RawSetString("TYPE_CLOSE", lua.LString(TypeClose))

	// Register close codes - create with exact known size
	numCloseCodes := len(closeCodes)
	closeCodesTable := l.CreateTable(0, numCloseCodes)
	for k, v := range closeCodes {
		closeCodesTable.RawSetString(k, lua.LNumber(v))
	}
	mod.RawSetString("CLOSE_CODES", closeCodesTable)

	value.RegisterMethods(l, "websocket.Client", map[string]lua.LGFunction{
		"send":    wsSend,
		"close":   wsClose,
		"receive": wsReceive,
	})

	l.Push(mod)
	return 1
}
