// SPDX-License-Identifier: MPL-2.0

package websocket

import (
	"context"
	"sync"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	wsapi "github.com/wippyai/runtime/api/service/websocket"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
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
	New: func() any { return &WsConnectYield{} },
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
	return wsapi.Connect
}

func (y *WsConnectYield) ToCommand() dispatcher.Command {
	return wsapi.ConnectCmd{
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
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, err.Error()).WithKind(lua.Internal).WithRetryable(true)}
	}

	id, ok := data.(uint64)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid connection ID type").WithKind(lua.Internal).WithRetryable(false)}
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
	Data        []byte
	ConnID      uint64
	MessageType int
}

var wsSendYieldPool = sync.Pool{
	New: func() any { return &WsSendYield{} },
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
	return wsapi.Send
}

func (y *WsSendYield) ToCommand() dispatcher.Command {
	return wsapi.SendCmd{ConnID: y.ConnID, Data: y.Data, MessageType: y.MessageType}
}

func (y *WsSendYield) Release() { ReleaseWsSendYield(y) }

// WsSubscribeYield is yielded to start receiving messages on a channel.
// This subscribes the channel to the topic and starts the dispatcher read loop.
type WsSubscribeYield struct {
	Channel *engine.Channel
	Conn    *WsConn
	PID     pid.PID
	Topic   string
	ConnID  uint64
}

var wsSubscribeYieldPool = sync.Pool{
	New: func() any { return &WsSubscribeYield{} },
}

func AcquireWsSubscribeYield(connID uint64, ch *engine.Channel, p pid.PID, topic string, conn *WsConn) *WsSubscribeYield {
	y := wsSubscribeYieldPool.Get().(*WsSubscribeYield)
	y.ConnID = connID
	y.Channel = ch
	y.PID = p
	y.Topic = topic
	y.Conn = conn
	return y
}

func ReleaseWsSubscribeYield(y *WsSubscribeYield) {
	y.ConnID = 0
	y.Channel = nil
	y.PID = pid.PID{}
	y.Topic = ""
	y.Conn = nil
	wsSubscribeYieldPool.Put(y)
}

func (y *WsSubscribeYield) String() string       { return "<ws_subscribe_yield>" }
func (y *WsSubscribeYield) Type() lua.LValueType { return lua.LTUserData }

func (y *WsSubscribeYield) CmdID() dispatcher.CommandID {
	return wsapi.Subscribe
}

func (y *WsSubscribeYield) ToCommand() dispatcher.Command {
	return wsapi.SubscribeCmd{ConnID: y.ConnID, PID: y.PID, Topic: y.Topic}
}

func (y *WsSubscribeYield) Release() { ReleaseWsSubscribeYield(y) }

// HandleResult implements HandledYield to set up topic subscription.
// This registers the channel for the topic and sets up a handler
// to convert incoming payloads to Lua message tables.
func (y *WsSubscribeYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, err.Error()).WithKind(lua.Internal).WithRetryable(true)}
	}

	proc := engine.GetProcess(l)
	if proc == nil {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "no process context").WithKind(lua.Internal).WithRetryable(false)}
	}

	// Register the externally-owned channel as an ordered producer-fed stream.
	// On overflow the engine reclaims the subscription and fires the
	// producer-stop cleanup rather than buffering an unbounded backlog.
	if err := proc.SubscribeExistingStream(y.Topic, y.Channel); err != nil {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, err.Error()).WithKind(lua.Internal).WithRetryable(true)}
	}

	// Set topic handler to convert websocket payloads to Lua tables
	proc.SetTopicHandler(y.Topic, wsMessageHandler)

	// Wire the dispatcher read-loop cancel as the subscription cleanup so
	// closeChannel / drain / Abort halt the producer goroutine when the
	// subscription is reclaimed (remote close terminal, conn:close(), or
	// process drain).
	if sub, ok := data.(wsapi.Subscription); ok && sub.Stop != nil {
		proc.SetSubscriptionCleanup(y.Channel, sub.Stop)
	}

	// Mark connection as subscribed
	if y.Conn != nil {
		y.Conn.subscribed = true
	}

	// Return the channel
	return []lua.LValue{y.Channel.Value()}
}

// wsMessageHandler converts websocket message payloads to Lua tables.
// Terminal payloads are handled by the process layer (closes channel automatically).
func wsMessageHandler(_ context.Context, l *lua.LState, _ pid.PID, _ string, payloads []payload.Payload) lua.LValue {
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
	Conn   *WsConn
	Reason string
	ConnID uint64
	Code   int
}

var wsCloseYieldPool = sync.Pool{
	New: func() any { return &WsCloseYield{} },
}

func AcquireWsCloseYield(connID uint64, code int, reason string, conn *WsConn) *WsCloseYield {
	y := wsCloseYieldPool.Get().(*WsCloseYield)
	y.ConnID = connID
	y.Code = code
	y.Reason = reason
	y.Conn = conn
	return y
}

func ReleaseWsCloseYield(y *WsCloseYield) {
	y.ConnID = 0
	y.Code = 0
	y.Reason = ""
	y.Conn = nil
	wsCloseYieldPool.Put(y)
}

func (y *WsCloseYield) String() string       { return "<ws_close_yield>" }
func (y *WsCloseYield) Type() lua.LValueType { return lua.LTUserData }

func (y *WsCloseYield) CmdID() dispatcher.CommandID {
	return wsapi.Close
}

func (y *WsCloseYield) ToCommand() dispatcher.Command {
	return wsapi.CloseCmd{ConnID: y.ConnID, Code: y.Code, Reason: y.Reason}
}

// HandleResult reclaims the connection's subscription on the step thread when
// the channel was subscribed. closeChannel removes the topic handler, closes
// the channel, and fires the producer-stop cleanup (the dispatcher read-loop
// cancel) so conn:close() leaves no live subscription or read-loop goroutine.
// close() returns no values per the module contract.
func (y *WsCloseYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, err.Error()).WithKind(lua.Internal).WithRetryable(false)}
	}
	if y.Conn != nil && y.Conn.subscribed && y.Conn.Channel != nil {
		if proc := engine.GetProcess(l); proc != nil {
			proc.UnsubscribeChannel(y.Conn.Channel)
		}
		y.Conn.subscribed = false
	}
	return nil
}

func (y *WsCloseYield) Release() { ReleaseWsCloseYield(y) }

// WsPingYield is yielded to ping the connection.
type WsPingYield struct {
	Data   []byte
	ConnID uint64
}

var wsPingYieldPool = sync.Pool{
	New: func() any { return &WsPingYield{} },
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
	return wsapi.Ping
}

func (y *WsPingYield) ToCommand() dispatcher.Command {
	return wsapi.PingCmd{ConnID: y.ConnID, Data: y.Data}
}

func (y *WsPingYield) Release() { ReleaseWsPingYield(y) }
