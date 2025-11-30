// Package ws provides WebSocket command handlers for the dispatcher system.
package ws

import (
	"context"
	"errors"
	"net/http"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	wsapi "github.com/wippyai/runtime/api/dispatcher/ws"

	"github.com/coder/websocket"
)

// WsRegistryKey is the context key for WsRegistry.
var WsRegistryKey = &ctxapi.Key{Name: "ws.registry", Inherit: false}

// Errors
var (
	ErrConnNotFound = errors.New("websocket connection not found")
	ErrConnClosed   = errors.New("websocket connection closed")
)

// connEntry holds an active WebSocket connection.
type connEntry struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	closed bool
}

// WsRegistry manages active WebSocket connections for a process.
type WsRegistry struct {
	mu     sync.Mutex
	conns  map[uint64]*connEntry
	nextID uint64
}

// NewWsRegistry creates a new WebSocket registry.
func NewWsRegistry() *WsRegistry {
	return &WsRegistry{
		conns: make(map[uint64]*connEntry),
	}
}

// Register adds a connection to the registry.
func (r *WsRegistry) Register(conn *websocket.Conn) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID++
	id := r.nextID

	r.conns[id] = &connEntry{
		conn:   conn,
		closed: false,
	}
	return id
}

// Get returns a connection by ID.
func (r *WsRegistry) Get(id uint64) (*connEntry, error) {
	r.mu.Lock()
	entry, ok := r.conns[id]
	r.mu.Unlock()

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
	r.mu.Lock()
	entry, ok := r.conns[id]
	if ok {
		delete(r.conns, id)
	}
	r.mu.Unlock()

	if !ok {
		return ErrConnNotFound
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

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

// CloseAll closes all connections.
func (r *WsRegistry) CloseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, entry := range r.conns {
		entry.mu.Lock()
		if !entry.closed {
			entry.closed = true
			entry.conn.Close(websocket.StatusGoingAway, "process terminating")
		}
		entry.mu.Unlock()
		delete(r.conns, id)
	}
}

// GetWsRegistry retrieves WsRegistry from FrameContext.
func GetWsRegistry(ctx context.Context) *WsRegistry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(WsRegistryKey); ok {
		return val.(*WsRegistry)
	}
	return nil
}

// SetWsRegistry stores WsRegistry in FrameContext.
func SetWsRegistry(ctx context.Context, r *WsRegistry) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(WsRegistryKey, r)
}

// GetOrCreateWsRegistry returns existing registry or creates a new one.
// Registers cleanup with FrameContext to close all connections on process termination.
func GetOrCreateWsRegistry(ctx context.Context) *WsRegistry {
	if r := GetWsRegistry(ctx); r != nil {
		return r
	}
	r := NewWsRegistry()
	_ = SetWsRegistry(ctx, r)

	// Register cleanup to close all connections when frame closes
	fc := ctxapi.FrameFromContext(ctx)
	if fc != nil {
		fc.AddCleanup(func() error {
			r.CloseAll()
			return nil
		})
	}

	return r
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

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.closed {
		return ErrConnClosed
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

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.closed {
		return ErrConnClosed
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

	entry.mu.Lock()
	if entry.closed {
		entry.mu.Unlock()
		emit(wsapi.WsMessage{EOF: true})
		return nil
	}
	entry.mu.Unlock()

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
