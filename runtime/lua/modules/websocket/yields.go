package websocket

import (
	"sync"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	wsapi "github.com/wippyai/runtime/api/dispatcher/ws"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// WsConnectYield is yielded to connect to a WebSocket server.
type WsConnectYield struct {
	URL                  string
	Headers              map[string]string
	DialTimeout          time.Duration
	CompressionMode      int
	CompressionThreshold int
	ReadLimit            int64
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
	y.DialTimeout = 0
	y.CompressionMode = 0
	y.CompressionThreshold = 0
	y.ReadLimit = 0
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
		DialTimeout:          y.DialTimeout,
		CompressionMode:      y.CompressionMode,
		CompressionThreshold: y.CompressionThreshold,
		ReadLimit:            y.ReadLimit,
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

	// Create websocket channel that yields WsReceiveYield on receive
	wsCh := &WsChannel{ConnID: id}
	conn := &WsConn{ID: id, Channel: wsCh}

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

// WsReceiveYield is yielded to receive a message.
type WsReceiveYield struct {
	ConnID uint64
}

var wsReceiveYieldPool = sync.Pool{
	New: func() interface{} { return &WsReceiveYield{} },
}

func AcquireWsReceiveYield(connID uint64) *WsReceiveYield {
	y := wsReceiveYieldPool.Get().(*WsReceiveYield)
	y.ConnID = connID
	return y
}

func ReleaseWsReceiveYield(y *WsReceiveYield) {
	y.ConnID = 0
	wsReceiveYieldPool.Put(y)
}

func (y *WsReceiveYield) String() string       { return "<ws_receive_yield>" }
func (y *WsReceiveYield) Type() lua.LValueType { return lua.LTUserData }

func (y *WsReceiveYield) CmdID() dispatcher.CommandID {
	return wsapi.CmdWsReceive
}

func (y *WsReceiveYield) ToCommand() dispatcher.Command {
	return wsapi.WsReceiveCmd{ConnID: y.ConnID}
}

func (y *WsReceiveYield) Release() { ReleaseWsReceiveYield(y) }

// HandleResult implements HandledYield to convert WsMessage to Lua table.
func (y *WsReceiveYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LFalse}
	}

	msg, ok := data.(wsapi.WsMessage)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LFalse}
	}

	if msg.EOF {
		// Connection closed - return table with close type
		tbl := l.CreateTable(0, 2)
		tbl.RawSetString("type", lua.LString("close"))
		tbl.RawSetString("data", lua.LNil)
		return []lua.LValue{tbl, lua.LTrue}
	}

	tbl := l.CreateTable(0, 2)
	if msg.MessageType == wsapi.MessageText {
		tbl.RawSetString("type", lua.LString("text"))
	} else {
		tbl.RawSetString("type", lua.LString("binary"))
	}
	tbl.RawSetString("data", lua.LString(msg.Data))
	return []lua.LValue{tbl, lua.LTrue}
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
