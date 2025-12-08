package websocket

import (
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	wsapi "github.com/wippyai/runtime/api/websocket"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

// parseDuration parses a Lua value into time.Duration.
// Supports numbers (as milliseconds) and strings (Go duration format).
func parseDuration(lv lua.LValue) (time.Duration, bool) {
	switch v := lv.(type) {
	case lua.LString:
		d, err := time.ParseDuration(string(v))
		if err != nil {
			return 0, false
		}
		return d, true
	case lua.LNumber:
		return time.Duration(v) * time.Millisecond, true
	default:
		return 0, false
	}
}

const (
	wsConnTypeName = "websocket.Client"
)

// Module is the websocket module definition.
var Module = &luaapi.ModuleDef{
	Name:        "websocket",
	Description: "WebSocket client connections",
	Class:       []string{luaapi.ClassNetwork, luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	value.RegisterTypeMethods(nil, wsConnTypeName, nil, connMethods)

	mod := lua.CreateTable(0, 10)
	mod.RawSetString("connect", lua.LGoFunc(connect))

	// Message types (strings for v1 compatibility)
	mod.RawSetString("TYPE_TEXT", lua.LString("text"))
	mod.RawSetString("TYPE_BINARY", lua.LString("binary"))
	mod.RawSetString("TYPE_PING", lua.LString("ping"))
	mod.RawSetString("TYPE_PONG", lua.LString("pong"))
	mod.RawSetString("TYPE_CLOSE", lua.LString("close"))

	// Numeric constants for internal use
	mod.RawSetString("TEXT", lua.LNumber(wsapi.MessageText))
	mod.RawSetString("BINARY", lua.LNumber(wsapi.MessageBinary))

	// Compression modes
	compressionTbl := lua.CreateTable(0, 3)
	compressionTbl.RawSetString("DISABLED", lua.LNumber(wsapi.CompressionDisabled))
	compressionTbl.RawSetString("CONTEXT_TAKEOVER", lua.LNumber(wsapi.CompressionContextTakeover))
	compressionTbl.RawSetString("NO_CONTEXT", lua.LNumber(wsapi.CompressionNoContext))
	compressionTbl.Immutable = true
	mod.RawSetString("COMPRESSION", compressionTbl)

	// Close codes
	closeCodeTbl := lua.CreateTable(0, 16)
	closeCodeTbl.RawSetString("NORMAL", lua.LNumber(1000))
	closeCodeTbl.RawSetString("GOING_AWAY", lua.LNumber(1001))
	closeCodeTbl.RawSetString("PROTOCOL_ERROR", lua.LNumber(1002))
	closeCodeTbl.RawSetString("UNSUPPORTED_DATA", lua.LNumber(1003))
	closeCodeTbl.RawSetString("RESERVED", lua.LNumber(1004))
	closeCodeTbl.RawSetString("NO_STATUS", lua.LNumber(1005))
	closeCodeTbl.RawSetString("ABNORMAL_CLOSURE", lua.LNumber(1006))
	closeCodeTbl.RawSetString("INVALID_PAYLOAD", lua.LNumber(1007))
	closeCodeTbl.RawSetString("POLICY_VIOLATION", lua.LNumber(1008))
	closeCodeTbl.RawSetString("MESSAGE_TOO_BIG", lua.LNumber(1009))
	closeCodeTbl.RawSetString("MANDATORY_EXTENSION", lua.LNumber(1010))
	closeCodeTbl.RawSetString("INTERNAL_ERROR", lua.LNumber(1011))
	closeCodeTbl.RawSetString("SERVICE_RESTART", lua.LNumber(1012))
	closeCodeTbl.RawSetString("TRY_AGAIN_LATER", lua.LNumber(1013))
	closeCodeTbl.RawSetString("BAD_GATEWAY", lua.LNumber(1014))
	closeCodeTbl.RawSetString("TLS_HANDSHAKE", lua.LNumber(1015))
	closeCodeTbl.Immutable = true
	mod.RawSetString("CLOSE_CODES", closeCodeTbl)

	mod.Immutable = true

	yields := []luaapi.YieldType{
		{Sample: &WsConnectYield{}, CmdID: wsapi.CmdWsConnect},
		{Sample: &WsSendYield{}, CmdID: wsapi.CmdWsSend},
		{Sample: &WsCloseYield{}, CmdID: wsapi.CmdWsClose},
		{Sample: &WsPingYield{}, CmdID: wsapi.CmdWsPing},
		{Sample: &WsSubscribeYield{}, CmdID: wsapi.CmdWsSubscribe},
	}

	return mod, yields
}

func connect(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	// General permission check for websocket.connect capability
	if !security.IsAllowed(ctx, "websocket.connect", "", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("websocket connections not allowed"))
		return 2
	}

	url := l.CheckString(1)
	if url == "" {
		l.ArgError(1, "URL required")
		return 0
	}

	// URL-specific permission check
	if !security.IsAllowed(ctx, "websocket.connect.url", url, nil) {
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

		// Protocols (subprotocols)
		if protos := opts.RawGetString("protocols"); protos.Type() == lua.LTTable {
			protos.(*lua.LTable).ForEach(func(_, v lua.LValue) {
				if v.Type() == lua.LTString {
					yield.Protocols = append(yield.Protocols, v.String())
				}
			})
		}

		// Dial timeout (ms or duration string)
		if timeout := opts.RawGetString("dial_timeout"); timeout != lua.LNil {
			if d, ok := parseDuration(timeout); ok {
				yield.DialTimeout = d
			}
		}

		// Read timeout (ms or duration string)
		if timeout := opts.RawGetString("read_timeout"); timeout != lua.LNil {
			if d, ok := parseDuration(timeout); ok {
				yield.ReadTimeout = d
			}
		}

		// Write timeout (ms or duration string)
		if timeout := opts.RawGetString("write_timeout"); timeout != lua.LNil {
			if d, ok := parseDuration(timeout); ok {
				yield.WriteTimeout = d
			}
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

		// Channel capacity
		if capacity := opts.RawGetString("channel_capacity"); capacity.Type() == lua.LTNumber || capacity.Type() == lua.LTInteger {
			yield.ChannelCapacity = int(lua.LVAsNumber(capacity))
		}
	}

	l.Push(yield)
	return -1
}

// WsConn represents a websocket connection.
type WsConn struct {
	ID         uint64
	Channel    *engine.Channel
	subscribed bool
}

var connMethods = map[string]lua.LGoFunc{
	"send":    connSend,
	"receive": connChannel,
	"channel": connChannel,
	"close":   connClose,
	"ping":    connPing,
}

func checkConn(l *lua.LState) *WsConn {
	ud := l.CheckUserData(1)
	if conn, ok := ud.Value.(*WsConn); ok {
		return conn
	}
	l.ArgError(1, "websocket.Client expected")
	return nil
}

func connSend(l *lua.LState) int {
	conn := checkConn(l)
	data := l.CheckString(2)
	msgType := wsapi.MessageText
	if l.GetTop() >= 3 {
		msgType = int(l.CheckNumber(3))
	}

	yield := AcquireWsSendYield(conn.ID, []byte(data), msgType)
	l.Push(yield)
	return -1
}

// connChannel returns the channel for receiving messages.
// On first call, yields WsSubscribeYield to start the subscription.
// Subsequent calls return the cached channel directly.
func connChannel(l *lua.LState) int {
	conn := checkConn(l)

	// If already subscribed, return the channel directly
	if conn.subscribed && conn.Channel != nil {
		l.Push(conn.Channel.Value())
		return 1
	}

	// First call - need to subscribe
	if conn.Channel == nil {
		l.RaiseError("connection has no channel")
		return 0
	}

	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("no context")
		return 0
	}

	// Get PID from frame context
	pid, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.RaiseError("no process PID")
		return 0
	}

	// Generate unique topic for this connection
	topic := fmt.Sprintf("ws@%d", conn.ID)

	yield := AcquireWsSubscribeYield(conn.ID, conn.Channel, pid, topic, conn)
	l.Push(yield)
	return -1
}

func connPing(l *lua.LState) int {
	conn := checkConn(l)
	yield := AcquireWsPingYield(conn.ID, nil)
	l.Push(yield)
	return -1
}

func connClose(l *lua.LState) int {
	conn := checkConn(l)
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
