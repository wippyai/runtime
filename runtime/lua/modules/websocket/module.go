package websocket

import (
	"sync"
	"time"

	wsapi "github.com/wippyai/runtime/api/dispatcher/ws"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable   *lua.LTable
	registration  *lua2api.Registration
	connMetatable *lua.LTable
	initOnce      sync.Once
)

const wsConnTypeName = "websocket.Client"

// Module is the singleton websocket module instance.
var Module = &websocketModule{}

type websocketModule struct{}

func (m *websocketModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "websocket",
		Description: "WebSocket client connections",
		Class:       []string{luaapi.ClassNetwork, luaapi.ClassIO, luaapi.ClassNondeterministic},
	}
}

func (m *websocketModule) Register(l *lua.LState) *lua2api.Registration {
	initOnce.Do(func() {
		moduleTable = createModuleTable()
		connMetatable = value.RegisterTypeMethods(nil, wsConnTypeName, nil, connMethods)
		registration = &lua2api.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	return registration
}

func (m *websocketModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use lua2api.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	lua2api.LoadModule(l, Module)
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 8)

	mod.RawSetString("connect", lua.LGoFunc(connect))

	registerConstants(mod)

	mod.Immutable = true
	return mod
}

func registerConstants(mod *lua.LTable) {
	// Message types (strings for engine1 compatibility)
	mod.RawSetString("TYPE_TEXT", lua.LString("text"))
	mod.RawSetString("TYPE_BINARY", lua.LString("binary"))
	mod.RawSetString("TYPE_PING", lua.LString("ping"))
	mod.RawSetString("TYPE_PONG", lua.LString("pong"))
	mod.RawSetString("TYPE_CLOSE", lua.LString("close"))

	// Also keep numeric constants for internal use
	mod.RawSetString("TEXT", lua.LNumber(wsapi.MessageText))
	mod.RawSetString("BINARY", lua.LNumber(wsapi.MessageBinary))

	// Compression modes
	compressionTbl := lua.CreateTable(0, 3)
	compressionTbl.RawSetString("DISABLED", lua.LNumber(wsapi.CompressionDisabled))
	compressionTbl.RawSetString("CONTEXT_TAKEOVER", lua.LNumber(wsapi.CompressionContextTakeover))
	compressionTbl.RawSetString("NO_CONTEXT", lua.LNumber(wsapi.CompressionNoContext))
	compressionTbl.Immutable = true
	mod.RawSetString("COMPRESSION", compressionTbl)

	closeCodes := map[string]int{
		"NORMAL":              1000,
		"GOING_AWAY":          1001,
		"PROTOCOL_ERROR":      1002,
		"UNSUPPORTED_DATA":    1003,
		"RESERVED":            1004,
		"NO_STATUS":           1005,
		"ABNORMAL_CLOSURE":    1006,
		"INVALID_PAYLOAD":     1007,
		"POLICY_VIOLATION":    1008,
		"MESSAGE_TOO_BIG":     1009,
		"MANDATORY_EXTENSION": 1010,
		"INTERNAL_ERROR":      1011,
		"SERVICE_RESTART":     1012,
		"TRY_AGAIN_LATER":     1013,
		"BAD_GATEWAY":         1014,
		"TLS_HANDSHAKE":       1015,
	}
	closeCodeTbl := lua.CreateTable(0, len(closeCodes))
	for name, code := range closeCodes {
		closeCodeTbl.RawSetString(name, lua.LNumber(code))
	}
	closeCodeTbl.Immutable = true
	mod.RawSetString("CLOSE_CODES", closeCodeTbl)
}

func connect(l *lua.LState) int {
	url := l.CheckString(1)
	if url == "" {
		l.ArgError(1, "URL required")
		return 0
	}

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	if !security.IsAllowed(ctx, "websocket.connect", url, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed: " + url))
		return 2
	}

	yield := AcquireWsConnectYield()
	yield.URL = url

	if l.GetTop() >= 2 && l.Get(2) != lua.LNil {
		opts := l.CheckTable(2)

		// Headers
		if hdrs := opts.RawGetString("headers"); hdrs.Type() == lua.LTTable {
			yield.Headers = make(map[string]string)
			hdrs.(*lua.LTable).ForEach(func(k, v lua.LValue) {
				if k.Type() == lua.LTString && v.Type() == lua.LTString {
					yield.Headers[k.String()] = v.String()
				}
			})
		}

		// Dial timeout (in seconds)
		if timeout := opts.RawGetString("dial_timeout"); timeout.Type() == lua.LTNumber || timeout.Type() == lua.LTInteger {
			yield.DialTimeout = time.Duration(lua.LVAsNumber(timeout) * lua.LNumber(time.Second))
		}

		// Compression mode (supports both numbers and strings)
		if compression := opts.RawGetString("compression"); compression != lua.LNil {
			switch compression.Type() {
			case lua.LTNumber, lua.LTInteger:
				yield.CompressionMode = int(lua.LVAsNumber(compression))
			case lua.LTString:
				switch compression.String() {
				case "context_takeover":
					yield.CompressionMode = wsapi.CompressionContextTakeover
				case "no_context_takeover":
					yield.CompressionMode = wsapi.CompressionNoContext
				case "disabled":
					yield.CompressionMode = wsapi.CompressionDisabled
				}
			}
		}

		// Compression threshold
		if threshold := opts.RawGetString("compression_threshold"); threshold.Type() == lua.LTNumber || threshold.Type() == lua.LTInteger {
			yield.CompressionThreshold = int(lua.LVAsNumber(threshold))
		}

		// Read limit
		if limit := opts.RawGetString("read_limit"); limit.Type() == lua.LTNumber || limit.Type() == lua.LTInteger {
			yield.ReadLimit = int64(lua.LVAsNumber(limit))
		}
	}

	l.Push(yield)
	return -1
}

var connMethods = map[string]lua.LGFunction{
	"send":      connSend,
	"receive":   connReceive,
	"close":     connClose,
	"subscribe": connSubscribe,
}

type WsConn struct {
	ID uint64
}

func checkConn(l *lua.LState, idx int) *WsConn {
	ud := l.CheckUserData(idx)
	if conn, ok := ud.Value.(*WsConn); ok {
		return conn
	}
	l.ArgError(idx, "websocket.Client expected")
	return nil
}

func NewConn(l *lua.LState, id uint64) lua.LValue {
	return value.NewUserData(l, &WsConn{ID: id}, connMetatable)
}

func connSend(l *lua.LState) int {
	conn := checkConn(l, 1)
	data := l.CheckString(2)
	msgType := wsapi.MessageText
	if l.GetTop() >= 3 {
		msgType = int(l.CheckNumber(3))
	}

	yield := AcquireWsSendYield(conn.ID, []byte(data), msgType)
	l.Push(yield)
	return -1
}

func connReceive(l *lua.LState) int {
	conn := checkConn(l, 1)

	yield := AcquireWsReceiveYield(conn.ID)
	l.Push(yield)
	return -1
}

func connClose(l *lua.LState) int {
	conn := checkConn(l, 1)
	code := 1000
	reason := ""
	if l.GetTop() >= 2 {
		code = int(l.CheckNumber(2))
	}
	if l.GetTop() >= 3 {
		reason = l.CheckString(3)
	}

	yield := AcquireWsCloseYield(conn.ID, code, reason)
	l.Push(yield)
	return -1
}

// connSubscribe starts a background read loop that delivers messages via emit.
// The Lua code should use this with a spawned task that handles incoming messages.
func connSubscribe(l *lua.LState) int {
	conn := checkConn(l, 1)

	yield := AcquireWsSubscribeYield(conn.ID)
	l.Push(yield)
	return -1
}
