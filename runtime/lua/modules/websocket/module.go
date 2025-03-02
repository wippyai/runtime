package websocket

import (
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
func (m *Module) Loader(l *lua.LState) int {
	mod := l.NewTable()

	// Register connection function
	l.SetField(mod, "connect", l.NewFunction(wsConnect))

	// Register message types
	l.SetField(mod, "TYPE_TEXT", lua.LString(TypeText))
	l.SetField(mod, "TYPE_BINARY", lua.LString(TypeBinary))
	l.SetField(mod, "TYPE_PING", lua.LString(TypePing))
	l.SetField(mod, "TYPE_PONG", lua.LString(TypePong))
	l.SetField(mod, "TYPE_CLOSE", lua.LString(TypeClose))

	// Register close codes
	closeCodesTable := l.NewTable()
	for k, v := range closeCodes {
		l.SetField(closeCodesTable, k, lua.LNumber(v))
	}
	l.SetField(mod, "CLOSE_CODES", closeCodesTable)

	// Register client methods
	mt := l.NewTypeMetatable("websocket.Client")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"send":    wsSend,
		"close":   wsClose,
		"receive": wsReceive,
	}))

	l.Push(mod)
	return 1
}
