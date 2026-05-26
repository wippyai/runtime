// SPDX-License-Identifier: MPL-2.0

package websocket

import (
	"fmt"
	"sync/atomic"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	wsapi "github.com/wippyai/runtime/api/service/websocket"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/security"
)

// subscriptionCounter generates globally-unique relay topics. The connection
// ID is a recyclable resource handle, so a per-connID topic (ws@<id>) would
// collide with a prior closed connection that reused the same handle: a stale
// terminal from the previous read loop could reclaim the new subscription.
// A monotonic counter keeps each subscription's topic distinct.
var subscriptionCounter uint64

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

// safeInt converts a Lua number to int with bounds checking.
// Returns the value and true if valid, or 0 and false if out of range.
func safeInt(lv lua.LValue, minVal, maxVal int) (int, bool) {
	n := float64(lua.LVAsNumber(lv))
	if n < float64(minVal) || n > float64(maxVal) {
		return 0, false
	}
	return int(n), true
}

// safeInt64 converts a Lua number to int64 with bounds checking.
func safeInt64(lv lua.LValue, minVal, maxVal int64) (int64, bool) {
	n := float64(lua.LVAsNumber(lv))
	if n < float64(minVal) || n > float64(maxVal) {
		return 0, false
	}
	return int64(n), true
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
	Types:       ModuleTypes,
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
		{Sample: &WsConnectYield{}, CmdID: wsapi.Connect},
		{Sample: &WsSendYield{}, CmdID: wsapi.Send},
		{Sample: &WsCloseYield{}, CmdID: wsapi.Close},
		{Sample: &WsPingYield{}, CmdID: wsapi.Ping},
		{Sample: &WsSubscribeYield{}, CmdID: wsapi.Subscribe},
	}

	return mod, yields
}

func connect(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	// General permission check for websocket.connect capability
	if !security.IsAllowed(ctx, "websocket.connect", "", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "websocket connections not allowed").WithKind(lua.PermissionDenied).WithRetryable(false))
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
		l.Push(lua.NewLuaError(l, "not allowed: "+url).WithKind(lua.PermissionDenied).WithRetryable(false))
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
				if v, ok := safeInt(compression, 0, 2); ok {
					yield.CompressionMode = v
				}
			case lua.LTString:
				switch compression.String() {
				case "context_takeover":
					yield.CompressionMode = wsapi.CompressionContextTakeover
				case "no_context_takeover":
					yield.CompressionMode = wsapi.CompressionNoContext
				case "disabled":
					yield.CompressionMode = wsapi.CompressionDisabled
				}
			case lua.LTNil, lua.LTBool, lua.LTTable, lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTChannel:
				// ignore unsupported types
			}
		}

		// Compression threshold (must be positive, max 100MB)
		if threshold := opts.RawGetString("compression_threshold"); threshold.Type() == lua.LTNumber || threshold.Type() == lua.LTInteger {
			if v, ok := safeInt(threshold, 0, 100*1024*1024); ok {
				yield.CompressionThreshold = v
			}
		}

		// Read limit (must be positive, max 128MB)
		if limit := opts.RawGetString("read_limit"); limit.Type() == lua.LTNumber || limit.Type() == lua.LTInteger {
			if v, ok := safeInt64(limit, 0, 128*1024*1024); ok {
				yield.ReadLimit = v
			}
		}

		// Channel capacity (must be positive, max 10000)
		if capacity := opts.RawGetString("channel_capacity"); capacity.Type() == lua.LTNumber || capacity.Type() == lua.LTInteger {
			if v, ok := safeInt(capacity, 1, 10000); ok {
				yield.ChannelCapacity = v
			}
		}
	}

	l.Push(yield)
	return -1
}

// WsConn represents a websocket connection.
type WsConn struct {
	Channel    *engine.Channel
	ID         uint64
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
	if conn == nil {
		return 0
	}
	data := l.CheckString(2)
	msgType := wsapi.MessageText
	if l.GetTop() >= 3 {
		if v, ok := safeInt(l.Get(3), wsapi.MessageText, wsapi.MessageBinary); ok {
			msgType = v
		}
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
	if conn == nil {
		return 0
	}

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

	// Generate a globally-unique topic, decoupled from the recyclable conn ID.
	topic := fmt.Sprintf("ws@%d", atomic.AddUint64(&subscriptionCounter, 1))

	yield := AcquireWsSubscribeYield(conn.ID, conn.Channel, pid, topic, conn)
	l.Push(yield)
	return -1
}

func connPing(l *lua.LState) int {
	conn := checkConn(l)
	if conn == nil {
		return 0
	}
	yield := AcquireWsPingYield(conn.ID, nil)
	l.Push(yield)
	return -1
}

func connClose(l *lua.LState) int {
	conn := checkConn(l)
	if conn == nil {
		return 0
	}
	code := 1000
	reason := ""
	if l.GetTop() >= 2 {
		// Valid WebSocket close codes: 1000-1015, 3000-4999
		if v, ok := safeInt(l.Get(2), 1000, 4999); ok {
			code = v
		}
	}
	if l.GetTop() >= 3 {
		reason = l.CheckString(3)
	}

	yield := AcquireWsCloseYield(conn.ID, code, reason, conn)
	l.Push(yield)
	return -1
}
