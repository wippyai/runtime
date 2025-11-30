// Package ws provides WebSocket command handlers for the dispatcher system.
package ws

import (
	"context"
	"errors"
	"net/http"

	"github.com/wippyai/runtime/api/dispatcher"
	wsapi "github.com/wippyai/runtime/api/dispatcher/ws"
	"github.com/wippyai/runtime/api/resource"

	"github.com/coder/websocket"
)

// TypeWsConn is the type ID for WebSocket connections in the resource table.
const TypeWsConn uint32 = 0x20

// Errors
var (
	ErrConnNotFound = errors.New("websocket connection not found")
	ErrConnClosed   = errors.New("websocket connection closed")
)

// connEntry holds an active WebSocket connection.
type connEntry struct {
	conn   *websocket.Conn
	closed bool
}

// Drop implements resource.Dropper for automatic cleanup.
func (e *connEntry) Drop() {
	if !e.closed {
		e.closed = true
		e.conn.Close(websocket.StatusGoingAway, "resource dropped")
	}
}

// WsRegistry manages active WebSocket connections using the resource table.
type WsRegistry struct {
	conns *resource.TypedTable[*connEntry]
}

// NewWsRegistry creates a WebSocket registry backed by the given table.
func NewWsRegistry(table *resource.Table) *WsRegistry {
	return &WsRegistry{
		conns: resource.NewTypedTable[*connEntry](table, TypeWsConn),
	}
}

// Register adds a connection to the registry.
func (r *WsRegistry) Register(conn *websocket.Conn) uint64 {
	entry := &connEntry{
		conn:   conn,
		closed: false,
	}
	handle := r.conns.Insert(entry)
	return uint64(handle)
}

// Get returns a connection entry by ID.
func (r *WsRegistry) Get(id uint64) (*connEntry, error) {
	entry, ok := r.conns.Get(resource.Handle(id))
	if !ok {
		return nil, ErrConnNotFound
	}
	if entry.closed {
		return nil, ErrConnClosed
	}
	return entry, nil
}

// Close closes a connection.
func (r *WsRegistry) Close(id uint64, code int, reason string) error {
	entry, ok := r.conns.Remove(resource.Handle(id))
	if !ok {
		return ErrConnNotFound
	}

	if entry.closed {
		return nil
	}
	entry.closed = true

	statusCode := websocket.StatusNormalClosure
	if code > 0 {
		statusCode = websocket.StatusCode(code)
	}

	return entry.conn.Close(statusCode, reason)
}

// GetWsRegistry returns a WsRegistry backed by the Table from context.
// Returns nil if no Table is available in the context.
func GetWsRegistry(ctx context.Context) *WsRegistry {
	table := resource.GetTable(ctx)
	if table == nil {
		return nil
	}
	return NewWsRegistry(table)
}

// GetOrCreateWsRegistry returns a WsRegistry for the context.
// Panics if no Store is available - engines must set resource.Store during initialization.
func GetOrCreateWsRegistry(ctx context.Context) *WsRegistry {
	registry := GetWsRegistry(ctx)
	if registry == nil {
		panic("ws: no resource.Store in context - engine must set it during initialization")
	}
	return registry
}

// WsConnectHandler connects to a WebSocket server.
type WsConnectHandler struct{}

func NewWsConnectHandler() *WsConnectHandler {
	return &WsConnectHandler{}
}

func (h *WsConnectHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	connectCmd := cmd.(wsapi.WsConnectCmd)

	opts := &websocket.DialOptions{}

	// Headers
	if len(connectCmd.Headers) > 0 {
		opts.HTTPHeader = make(http.Header)
		for k, v := range connectCmd.Headers {
			opts.HTTPHeader.Set(k, v)
		}
	}

	// Compression
	switch connectCmd.CompressionMode {
	case wsapi.CompressionContextTakeover:
		opts.CompressionMode = websocket.CompressionContextTakeover
	case wsapi.CompressionNoContext:
		opts.CompressionMode = websocket.CompressionNoContextTakeover
	default:
		opts.CompressionMode = websocket.CompressionDisabled
	}
	if connectCmd.CompressionThreshold > 0 {
		opts.CompressionThreshold = connectCmd.CompressionThreshold
	}

	// Dial timeout
	dialCtx := ctx
	if connectCmd.DialTimeout > 0 {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeout(ctx, connectCmd.DialTimeout)
		defer cancel()
	}

	conn, _, err := websocket.Dial(dialCtx, connectCmd.URL, opts)
	if err != nil {
		return err
	}

	// Read limit
	if connectCmd.ReadLimit > 0 {
		conn.SetReadLimit(connectCmd.ReadLimit)
	}

	registry := GetOrCreateWsRegistry(ctx)
	id := registry.Register(conn)

	emit(id)
	return nil
}

// WsSendHandler sends a message on a WebSocket connection.
type WsSendHandler struct{}

func NewWsSendHandler() *WsSendHandler {
	return &WsSendHandler{}
}

func (h *WsSendHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	sendCmd := cmd.(wsapi.WsSendCmd)

	registry := GetWsRegistry(ctx)
	if registry == nil {
		return ErrConnNotFound
	}

	entry, err := registry.Get(sendCmd.ConnID)
	if err != nil {
		return err
	}

	msgType := websocket.MessageText
	if sendCmd.MessageType == wsapi.MessageBinary {
		msgType = websocket.MessageBinary
	}

	return entry.conn.Write(ctx, msgType, sendCmd.Data)
}

// WsReceiveHandler receives a message from a WebSocket connection.
type WsReceiveHandler struct{}

func NewWsReceiveHandler() *WsReceiveHandler {
	return &WsReceiveHandler{}
}

func (h *WsReceiveHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	recvCmd := cmd.(wsapi.WsReceiveCmd)

	registry := GetWsRegistry(ctx)
	if registry == nil {
		return ErrConnNotFound
	}

	entry, err := registry.Get(recvCmd.ConnID)
	if err != nil {
		return err
	}

	msgType, data, err := entry.conn.Read(ctx)
	if err != nil {
		closeStatus := websocket.CloseStatus(err)
		if closeStatus >= 0 {
			emit(wsapi.WsMessage{
				Data:        nil,
				MessageType: 0,
				EOF:         true,
			})
			return nil
		}
		return err
	}

	mt := wsapi.MessageText
	if msgType == websocket.MessageBinary {
		mt = wsapi.MessageBinary
	}

	emit(wsapi.WsMessage{
		Data:        data,
		MessageType: mt,
		EOF:         false,
	})
	return nil
}

// WsCloseHandler closes a WebSocket connection.
type WsCloseHandler struct{}

func NewWsCloseHandler() *WsCloseHandler {
	return &WsCloseHandler{}
}

func (h *WsCloseHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	closeCmd := cmd.(wsapi.WsCloseCmd)

	registry := GetWsRegistry(ctx)
	if registry == nil {
		return ErrConnNotFound
	}

	return registry.Close(closeCmd.ConnID, closeCmd.Code, closeCmd.Reason)
}

// WsPingHandler sends a ping on the connection.
type WsPingHandler struct{}

func NewWsPingHandler() *WsPingHandler {
	return &WsPingHandler{}
}

func (h *WsPingHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	pingCmd := cmd.(wsapi.WsPingCmd)

	registry := GetWsRegistry(ctx)
	if registry == nil {
		return ErrConnNotFound
	}

	entry, err := registry.Get(pingCmd.ConnID)
	if err != nil {
		return err
	}

	return entry.conn.Ping(ctx)
}

// WsSubscribeHandler starts a background read loop for a WebSocket connection.
// Messages are delivered via emit until the connection closes.
type WsSubscribeHandler struct{}

func NewWsSubscribeHandler() *WsSubscribeHandler {
	return &WsSubscribeHandler{}
}

func (h *WsSubscribeHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	subCmd := cmd.(wsapi.WsSubscribeCmd)

	registry := GetWsRegistry(ctx)
	if registry == nil {
		return ErrConnNotFound
	}

	entry, err := registry.Get(subCmd.ConnID)
	if err != nil {
		return err
	}

	// Emit subscription confirmation
	emit(wsapi.WsSubscription(subCmd))

	// Spawn background read loop
	go func() {
		for {
			msgType, data, err := entry.conn.Read(ctx)
			if err != nil {
				closeStatus := websocket.CloseStatus(err)
				if closeStatus >= 0 || ctx.Err() != nil {
					emit(wsapi.WsMessage{EOF: true})
					return
				}
				emit(wsapi.WsMessage{EOF: true})
				return
			}

			mt := wsapi.MessageText
			if msgType == websocket.MessageBinary {
				mt = wsapi.MessageBinary
			}

			emit(wsapi.WsMessage{
				Data:        data,
				MessageType: mt,
				EOF:         false,
			})
		}
	}()

	return nil
}

// Service bundles all WebSocket handlers.
type Service struct {
	Connect   *WsConnectHandler
	Send      *WsSendHandler
	Receive   *WsReceiveHandler
	Close     *WsCloseHandler
	Ping      *WsPingHandler
	Subscribe *WsSubscribeHandler
}

// NewService creates a new WebSocket service with all handlers initialized.
func NewService() *Service {
	return &Service{
		Connect:   NewWsConnectHandler(),
		Send:      NewWsSendHandler(),
		Receive:   NewWsReceiveHandler(),
		Close:     NewWsCloseHandler(),
		Ping:      NewWsPingHandler(),
		Subscribe: NewWsSubscribeHandler(),
	}
}

// RegisterAll registers all WebSocket handlers with the given registry function.
func (s *Service) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(wsapi.CmdWsConnect, s.Connect)
	register(wsapi.CmdWsSend, s.Send)
	register(wsapi.CmdWsReceive, s.Receive)
	register(wsapi.CmdWsClose, s.Close)
	register(wsapi.CmdWsPing, s.Ping)
	register(wsapi.CmdWsSubscribe, s.Subscribe)
}
