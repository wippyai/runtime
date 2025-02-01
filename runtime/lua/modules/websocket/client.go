package websocket

import (
	"context"
	"fmt"
	"github.com/coder/websocket"
	"github.com/ponyruntime/pony/internal/closer"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"net/http"
	"sync"
	"time"
)

// MessageType represents the type of WebSocket message.
type MessageType string

// WebSocket message types.
const (
	TypeText   MessageType = "text"
	TypeBinary MessageType = "binary"
	TypePing   MessageType = "ping"
	TypePong   MessageType = "pong"
	TypeClose  MessageType = "close"
)

// WebSocket close codes.
const (
	CloseCodeNormal          = 1000
	CloseCodeGoingAway       = 1001
	CloseCodeProtocolError   = 1002
	CloseCodeUnsupportedData = 1003
	CloseCodeReserved        = 1004
	CloseCodeNoStatus        = 1005
	CloseCodeAbnormalClosure = 1006
	CloseCodeInvalidPayload  = 1007
	CloseCodePolicyViolation = 1008
	CloseCodeMessageTooBig   = 1009
	CloseCodeMandatoryExt    = 1010
	CloseCodeInternalError   = 1011
	CloseCodeServiceRestart  = 1012
	CloseCodeTryAgainLater   = 1013
	CloseCodeBadGateway      = 1014
	CloseCodeTLSHandshake    = 1015
)

var closeCodes = map[string]int{
	"NORMAL":              CloseCodeNormal,
	"GOING_AWAY":          CloseCodeGoingAway,
	"PROTOCOL_ERROR":      CloseCodeProtocolError,
	"UNSUPPORTED_DATA":    CloseCodeUnsupportedData,
	"RESERVED":            CloseCodeReserved,
	"NO_STATUS":           CloseCodeNoStatus,
	"ABNORMAL_CLOSURE":    CloseCodeAbnormalClosure,
	"INVALID_PAYLOAD":     CloseCodeInvalidPayload,
	"POLICY_VIOLATION":    CloseCodePolicyViolation,
	"MESSAGE_TOO_BIG":     CloseCodeMessageTooBig,
	"MANDATORY_EXTENSION": CloseCodeMandatoryExt,
	"INTERNAL_ERROR":      CloseCodeInternalError,
	"SERVICE_RESTART":     CloseCodeServiceRestart,
	"TRY_AGAIN_LATER":     CloseCodeTryAgainLater,
	"BAD_GATEWAY":         CloseCodeBadGateway,
	"TLS_HANDSHAKE":       CloseCodeTLSHandshake,
}

// wsClient wraps the underlying websocket connection and associated state.
type wsClient struct {
	conn         *websocket.Conn
	recvCh       *channel.Channel
	recvValue    lua.LValue
	readTimeout  time.Duration
	writeTimeout time.Duration
	closeOnce    sync.Once
}

// LuaWSClient is the userdata wrapper for wsClient.
type LuaWSClient struct {
	client *wsClient
}

// parseDuration parses a Lua value into a time.Duration.
func parseDuration(lv lua.LValue) (time.Duration, error) {
	switch v := lv.(type) {
	case lua.LString:
		return time.ParseDuration(string(v))
	case lua.LNumber:
		return time.Duration(v) * time.Millisecond, nil
	default:
		return 0, fmt.Errorf("invalid duration type")
	}
}

// wsConnect is the global function: websocket.connect.
func wsConnect(l *lua.LState) int {
	url := l.CheckString(1)
	var options *lua.LTable
	if l.GetTop() >= 2 {
		options = l.CheckTable(2)
	}

	// Default options.
	headers := http.Header{}
	var protocols []string
	var dialTimeout time.Duration
	var readTimeout time.Duration
	var writeTimeout time.Duration

	// New options:
	// channel_capacity: capacity for the receive channel (default 0)
	// compression: "context_takeover", "no_context_takeover", or "disabled" (default "disabled")
	// compression_threshold: threshold in bytes for compression (default 0)
	var channelCapacity int = 0
	var compressionMode websocket.CompressionMode = websocket.CompressionDisabled
	var compressionThreshold int = 0

	// Parse options table.
	if options != nil {
		options.ForEach(func(key, value lua.LValue) {
			switch key.String() {
			case "headers":
				if tbl, ok := value.(*lua.LTable); ok {
					tbl.ForEach(func(k, v lua.LValue) {
						headers.Add(k.String(), v.String())
					})
				}
			case "protocols":
				if tbl, ok := value.(*lua.LTable); ok {
					tbl.ForEach(func(_, v lua.LValue) {
						protocols = append(protocols, v.String())
					})
				}
			case "dial_timeout":
				if d, err := parseDuration(value); err == nil {
					dialTimeout = d
				}
			case "read_timeout":
				if d, err := parseDuration(value); err == nil {
					readTimeout = d
				}
			case "write_timeout":
				if d, err := parseDuration(value); err == nil {
					writeTimeout = d
				}
			case "channel_capacity":
				if n, ok := value.(lua.LNumber); ok {
					channelCapacity = int(n)
				}
			case "compression":
				switch value.String() {
				case "context_takeover":
					compressionMode = websocket.CompressionContextTakeover
				case "no_context_takeover":
					compressionMode = websocket.CompressionNoContextTakeover
				case "disabled":
					compressionMode = websocket.CompressionDisabled
				default:
					compressionMode = websocket.CompressionDisabled
				}
			case "compression_threshold":
				if n, ok := value.(lua.LNumber); ok {
					compressionThreshold = int(n)
				}
			}
		})
	}

	// Setup context with cleanup.
	ctx := l.Context()
	cleanup := closer.FromContext(ctx)
	if cleanup == nil {
		ctx, cleanup = closer.WithContext(ctx)
	}

	if dialTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, dialTimeout)
		cleanup.Add(func() error { cancel(); return nil })
	}

	opts := &websocket.DialOptions{
		HTTPHeader:           headers,
		Subprotocols:         protocols,
		CompressionMode:      compressionMode,
		CompressionThreshold: compressionThreshold,
	}

	// Establish connection.
	conn, _, err := websocket.Dial(ctx, url, opts)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create receive channel with a unique name using the configured capacity.
	recvCh := channel.Named(fmt.Sprintf("ws_%p", conn), channelCapacity)

	// Create client instance.
	client := &wsClient{
		conn:         conn,
		recvCh:       recvCh,
		readTimeout:  readTimeout,
		writeTimeout: writeTimeout,
	}

	// Create and store channel wrapper.
	client.recvValue = channel.Wrap(l, recvCh)

	// Add connection cleanup.
	cleanup.Add(func() error {
		client.closeOnce.Do(func() {
			_ = client.conn.Close(websocket.StatusGoingAway, "connection cleanup")
		})
		return nil
	})

	// Start read loop.
	go client.readLoop(l)

	// Create userdata and set metatable.
	ud := l.NewUserData()
	ud.Value = &LuaWSClient{client: client}
	l.SetMetatable(ud, l.GetTypeMetatable("WebSocketClient"))
	l.Push(ud)
	return 1
}

// readLoop continuously reads messages from the connection.
func (c *wsClient) readLoop(l *lua.LState) {
	ctx := l.Context()

	for {
		var readCtx context.Context
		var cancel context.CancelFunc

		if c.readTimeout > 0 {
			readCtx, cancel = context.WithTimeout(ctx, c.readTimeout)
		} else {
			readCtx = ctx
		}

		msgType, data, err := c.conn.Read(readCtx)
		if cancel != nil {
			cancel()
		}

		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				msgTbl := l.NewTable()
				l.SetField(msgTbl, "type", lua.LString(TypeClose))
				l.SetField(msgTbl, "code", lua.LNumber(CloseCodeNormal))
				l.SetField(msgTbl, "reason", lua.LString("normal closure"))
				_ = async.Send(l, c.recvCh, msgTbl, true)
				_ = async.Close(l, c.recvCh)
			}
			break
		}

		// Create message table.
		msgTbl := l.NewTable()
		switch msgType {
		case websocket.MessageText:
			l.SetField(msgTbl, "type", lua.LString(TypeText))
			l.SetField(msgTbl, "data", lua.LString(string(data)))
		case websocket.MessageBinary:
			l.SetField(msgTbl, "type", lua.LString(TypeBinary))
			l.SetField(msgTbl, "data", lua.LString(data))
		}

		_ = async.Send(l, c.recvCh, msgTbl, true)
	}
}

// wsSend implements client:send(data: string).
func wsSend(l *lua.LState) int {
	client, err := CheckWSClient(l)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	data := l.CheckString(2)

	ctx := l.Context()
	cleanup := closer.FromContext(ctx)

	if client.client.writeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, client.client.writeTimeout)
		cleanup.Add(func() error { cancel(); return nil })
	}

	if err := client.client.conn.Write(ctx, websocket.MessageText, []byte(data)); err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

// wsClose implements client:close(code?: number, reason?: string).
func wsClose(l *lua.LState) int {
	client, err := CheckWSClient(l)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	code := websocket.StatusNormalClosure
	if l.GetTop() >= 2 && !lua.LVIsFalse(l.Get(2)) {
		code = websocket.StatusCode(l.CheckNumber(2))
	}

	reason := ""
	if l.GetTop() >= 3 && !lua.LVIsFalse(l.Get(3)) {
		reason = l.CheckString(3)
	}

	var closeErr error
	client.client.closeOnce.Do(func() {
		ctx := l.Context()

		var cancel context.CancelFunc
		if client.client.writeTimeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, client.client.writeTimeout)
			defer cancel()
		}

		closeErr = client.client.conn.Close(code, reason)
	})

	if closeErr != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(closeErr.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

// wsReceive implements client:receive().
func wsReceive(l *lua.LState) int {
	client, err := CheckWSClient(l)
	if err != nil {
		l.RaiseError(err.Error())
		return 2
	}

	l.Push(client.client.recvValue)
	return 1
}

// CheckWSClient verifies that the userdata is a WebSocketClient.
func CheckWSClient(l *lua.LState) (*LuaWSClient, error) {
	ud := l.CheckUserData(1)
	if ws, ok := ud.Value.(*LuaWSClient); ok {
		return ws, nil
	}
	return nil, fmt.Errorf("expected WebSocketClient, got %T", ud.Value)
}
