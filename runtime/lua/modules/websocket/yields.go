package websocket

import (
	"context"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	wsapi "github.com/wippyai/runtime/api/dispatcher/ws"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// WsConnectYield is yielded to connect to a WebSocket server.
type WsConnectYield struct {
	URL                  string
	Headers              map[string]string
	Protocols            []string
	DialTimeout          time.Duration
	ReadTimeout          time.Duration
	WriteTimeout         time.Duration
	CompressionMode      int
	CompressionThreshold int
	ReadLimit            int64
	ChannelCapacity      int
}

var wsConnectYieldPool = sync.Pool{
	New: func() interface{} { return &WsConnectYield{} },
}

func AcquireWsConnectYield() *WsConnectYield {
	return wsConnectYieldPool.Get().(*WsConnectYield)
}

func ReleaseWsConnectYield(y *WsConnectYield) {
	y.URL = ""
	y.Headers = nil
	y.Protocols = nil
	y.DialTimeout = 0
	y.ReadTimeout = 0
	y.WriteTimeout = 0
	y.CompressionMode = 0
	y.CompressionThreshold = 0
	y.ReadLimit = 0
	y.ChannelCapacity = 0
	wsConnectYieldPool.Put(y)
}

func (y *WsConnectYield) String() string       { return "<ws_connect_yield>" }
func (y *WsConnectYield) Type() lua.LValueType { return lua.LTUserData }

func (y *WsConnectYield) CmdID() dispatcher.CommandID {
	return wsapi.CmdWsConnect
}

func (y *WsConnectYield) ToCommand() dispatcher.Command {
	return wsapi.WsConnectCmd{
		URL:                  y.URL,
		Headers:              y.Headers,
		Protocols:            y.Protocols,
		DialTimeout:          y.DialTimeout,
		ReadTimeout:          y.ReadTimeout,
		WriteTimeout:         y.WriteTimeout,
		CompressionMode:      y.CompressionMode,
		CompressionThreshold: y.CompressionThreshold,
		ReadLimit:            y.ReadLimit,
		ChannelCapacity:      y.ChannelCapacity,
	}
}

func (y *WsConnectYield) Release() { ReleaseWsConnectYield(y) }

// HandleResult implements HandledYield to convert connection ID to WsConn userdata.
func (y *WsConnectYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}

	id, ok := data.(uint64)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid connection ID type")}
	}

	// Create engine.Channel for receiving messages (works with channel.select)
	ch := engine.NewChannel(32)
	engine.PushChannel(l, ch)
	l.Pop(1) // PushChannel pushes to stack, we'll return via userdata

	conn := &WsConn{ID: id, Channel: ch}

	ud := l.NewUserData()
	ud.Value = conn
	ud.Metatable = value.GetTypeMetatable(l, wsConnTypeName)
	return []lua.LValue{ud}
}

// WsSendYield is yielded to send a message.
type WsSendYield struct {
	ConnID      uint64
	Data        []byte
	MessageType int
}

var wsSendYieldPool = sync.Pool{
	New: func() interface{} { return &WsSendYield{} },
}

func AcquireWsSendYield(connID uint64, data []byte, msgType int) *WsSendYield {
	y := wsSendYieldPool.Get().(*WsSendYield)
	y.ConnID = connID
	y.Data = data
	y.MessageType = msgType
	return y
}

func ReleaseWsSendYield(y *WsSendYield) {
	y.ConnID = 0
	y.Data = nil
	y.MessageType = 0
	wsSendYieldPool.Put(y)
}

func (y *WsSendYield) String() string       { return "<ws_send_yield>" }
func (y *WsSendYield) Type() lua.LValueType { return lua.LTUserData }

func (y *WsSendYield) CmdID() dispatcher.CommandID {
	return wsapi.CmdWsSend
}

func (y *WsSendYield) ToCommand() dispatcher.Command {
	return wsapi.WsSendCmd{ConnID: y.ConnID, Data: y.Data, MessageType: y.MessageType}
}

func (y *WsSendYield) Release() { ReleaseWsSendYield(y) }

// WsSubscribeYield is yielded to start receiving messages on a channel.
// This subscribes the channel to the topic and starts the dispatcher read loop.
type WsSubscribeYield struct {
	ConnID  uint64
	Channel *engine.Channel
	PID     relay.PID
	Topic   string
	Conn    *WsConn
}

var wsSubscribeYieldPool = sync.Pool{
	New: func() interface{} { return &WsSubscribeYield{} },
}

func AcquireWsSubscribeYield(connID uint64, ch *engine.Channel, pid relay.PID, topic string, conn *WsConn) *WsSubscribeYield {
	y := wsSubscribeYieldPool.Get().(*WsSubscribeYield)
	y.ConnID = connID
	y.Channel = ch
	y.PID = pid
	y.Topic = topic
	y.Conn = conn
	return y
}

func ReleaseWsSubscribeYield(y *WsSubscribeYield) {
	y.ConnID = 0
	y.Channel = nil
	y.PID = relay.PID{}
	y.Topic = ""
	y.Conn = nil
	wsSubscribeYieldPool.Put(y)
}

func (y *WsSubscribeYield) String() string       { return "<ws_subscribe_yield>" }
func (y *WsSubscribeYield) Type() lua.LValueType { return lua.LTUserData }

func (y *WsSubscribeYield) CmdID() dispatcher.CommandID {
	return wsapi.CmdWsSubscribe
}

func (y *WsSubscribeYield) ToCommand() dispatcher.Command {
	return wsapi.WsSubscribeCmd{ConnID: y.ConnID, PID: y.PID, Topic: y.Topic}
}

func (y *WsSubscribeYield) Release() { ReleaseWsSubscribeYield(y) }

// HandleResult implements HandledYield to set up topic subscription.
// This registers the channel for the topic and sets up a handler
// to convert incoming payloads to Lua message tables.
func (y *WsSubscribeYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}

	ctx := l.Context()
	pc := engine.GetProcessContext(ctx)
	if pc == nil {
		return []lua.LValue{lua.LNil, lua.LString("no process context")}
	}

	// Subscribe the channel to the topic
	if err := pc.Subscribe(y.Topic, y.Channel); err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}

	// Set topic handler to convert websocket payloads to Lua tables
	pc.SetTopicHandler(y.Topic, wsMessageHandler)

	// Mark connection as subscribed
	if y.Conn != nil {
		y.Conn.subscribed = true
	}

	// Return the channel
	return []lua.LValue{y.Channel.Value()}
}

// wsMessageHandler converts websocket message payloads to Lua tables.
// Terminal payloads are handled by the process layer (closes channel automatically).
func wsMessageHandler(ctx context.Context, l *lua.LState, payloads []payload.Payload) lua.LValue {
	if len(payloads) == 0 {
		return lua.LNil
	}

	p := payloads[0]

	// Create message table
	tbl := l.CreateTable(0, 2)
	if p.Format() == payload.String {
		tbl.RawSetString("type", lua.LString("text"))
	} else {
		tbl.RawSetString("type", lua.LString("binary"))
	}

	// Convert data to string
	switch v := p.Data().(type) {
	case string:
		tbl.RawSetString("data", lua.LString(v))
	case []byte:
		tbl.RawSetString("data", lua.LString(v))
	default:
		tbl.RawSetString("data", lua.LNil)
	}
	return tbl
}

// WsCloseYield is yielded to close a connection.
type WsCloseYield struct {
	ConnID uint64
	Code   int
	Reason string
}

var wsCloseYieldPool = sync.Pool{
	New: func() interface{} { return &WsCloseYield{} },
}

func AcquireWsCloseYield(connID uint64, code int, reason string) *WsCloseYield {
	y := wsCloseYieldPool.Get().(*WsCloseYield)
	y.ConnID = connID
	y.Code = code
	y.Reason = reason
	return y
}

func ReleaseWsCloseYield(y *WsCloseYield) {
	y.ConnID = 0
	y.Code = 0
	y.Reason = ""
	wsCloseYieldPool.Put(y)
}

func (y *WsCloseYield) String() string       { return "<ws_close_yield>" }
func (y *WsCloseYield) Type() lua.LValueType { return lua.LTUserData }

func (y *WsCloseYield) CmdID() dispatcher.CommandID {
	return wsapi.CmdWsClose
}

func (y *WsCloseYield) ToCommand() dispatcher.Command {
	return wsapi.WsCloseCmd{ConnID: y.ConnID, Code: y.Code, Reason: y.Reason}
}

func (y *WsCloseYield) Release() { ReleaseWsCloseYield(y) }

// WsPingYield is yielded to ping the connection.
type WsPingYield struct {
	ConnID uint64
	Data   []byte
}

var wsPingYieldPool = sync.Pool{
	New: func() interface{} { return &WsPingYield{} },
}

func AcquireWsPingYield(connID uint64, data []byte) *WsPingYield {
	y := wsPingYieldPool.Get().(*WsPingYield)
	y.ConnID = connID
	y.Data = data
	return y
}

func ReleaseWsPingYield(y *WsPingYield) {
	y.ConnID = 0
	y.Data = nil
	wsPingYieldPool.Put(y)
}

func (y *WsPingYield) String() string       { return "<ws_ping_yield>" }
func (y *WsPingYield) Type() lua.LValueType { return lua.LTUserData }

func (y *WsPingYield) CmdID() dispatcher.CommandID {
	return wsapi.CmdWsPing
}

func (y *WsPingYield) ToCommand() dispatcher.Command {
	return wsapi.WsPingCmd{ConnID: y.ConnID, Data: y.Data}
}

func (y *WsPingYield) Release() { ReleaseWsPingYield(y) }
