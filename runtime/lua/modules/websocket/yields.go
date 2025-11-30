package websocket

import (
	"sync"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	wsapi "github.com/wippyai/runtime/api/dispatcher/ws"
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

// WsSubscribeYield is yielded to subscribe to messages.
type WsSubscribeYield struct {
	ConnID uint64
}

var wsSubscribeYieldPool = sync.Pool{
	New: func() interface{} { return &WsSubscribeYield{} },
}

func AcquireWsSubscribeYield(connID uint64) *WsSubscribeYield {
	y := wsSubscribeYieldPool.Get().(*WsSubscribeYield)
	y.ConnID = connID
	return y
}

func ReleaseWsSubscribeYield(y *WsSubscribeYield) {
	y.ConnID = 0
	wsSubscribeYieldPool.Put(y)
}

func (y *WsSubscribeYield) String() string       { return "<ws_subscribe_yield>" }
func (y *WsSubscribeYield) Type() lua.LValueType { return lua.LTUserData }

func (y *WsSubscribeYield) CmdID() dispatcher.CommandID {
	return wsapi.CmdWsSubscribe
}

func (y *WsSubscribeYield) ToCommand() dispatcher.Command {
	return wsapi.WsSubscribeCmd{ConnID: y.ConnID}
}

func (y *WsSubscribeYield) Release() { ReleaseWsSubscribeYield(y) }
