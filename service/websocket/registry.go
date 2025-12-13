// Package websocket provides WebSocket command handlers for the dispatcher system.
package websocket

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/runtime/resource"
	wssvc "github.com/wippyai/runtime/api/service/websocket"
	wsapi "github.com/wippyai/runtime/api/websocket"

	"github.com/coder/websocket"
	"go.uber.org/zap"
)

// TypeWsConn is the type ID for WebSocket connections in the resource table.
const TypeWsConn uint32 = 0x20

// registryKey is the FrameContext key for caching the websocket Registry.
var registryKey = &ctxapi.Key{Name: "websocket.registry", Inherit: false}

// connEntry holds an active WebSocket connection with its message channel.
type connEntry struct {
	conn        *websocket.Conn
	msgCh       chan wsapi.WsMessage
	ctx         context.Context
	cancel      context.CancelFunc
	closed      atomic.Bool
	closeOnce   sync.Once
	readTimeout time.Duration
	log         *zap.Logger
}

// Drop implements resource.Dropper for automatic cleanup.
func (e *connEntry) Drop() {
	e.close(websocket.StatusGoingAway, "resource dropped")
}

// close closes the connection and stops the read loop.
func (e *connEntry) close(code websocket.StatusCode, reason string) {
	e.closeOnce.Do(func() {
		e.closed.Store(true)
		e.cancel()
		if err := e.conn.Close(code, reason); err != nil {
			e.log.Debug("connection close error", zap.Error(err))
		}
	})
}

// readLoop continuously reads messages from the websocket and sends to channel.
func (e *connEntry) readLoop() {
	defer func() {
		close(e.msgCh)
		e.close(websocket.StatusGoingAway, "read loop ended")
	}()

	for {
		select {
		case <-e.ctx.Done():
			return
		default:
		}

		readCtx := e.ctx
		var cancel context.CancelFunc
		if e.readTimeout > 0 {
			readCtx, cancel = context.WithTimeout(e.ctx, e.readTimeout)
		}

		msgType, data, err := e.conn.Read(readCtx)

		if cancel != nil {
			cancel()
		}

		if err != nil {
			closeStatus := websocket.CloseStatus(err)
			if closeStatus >= 0 || e.ctx.Err() != nil {
				select {
				case e.msgCh <- wsapi.WsMessage{EOF: true}:
				case <-e.ctx.Done():
				}
			}
			return
		}

		mt := wsapi.MessageText
		if msgType == websocket.MessageBinary {
			mt = wsapi.MessageBinary
		}

		msg := wsapi.WsMessage{
			Data:        data,
			MessageType: mt,
			EOF:         false,
		}

		select {
		case e.msgCh <- msg:
		case <-e.ctx.Done():
			return
		}
	}
}

var _ resource.Dropper = (*connEntry)(nil)

// Registry manages active WebSocket connections using the resource table.
type Registry struct {
	conns *resource.TypedTable[*connEntry]
	log   *zap.Logger
}

// NewRegistry creates a WebSocket registry backed by the given table.
func NewRegistry(table *resource.Table, log *zap.Logger) *Registry {
	if log == nil {
		log = zap.NewNop()
	}
	return &Registry{
		conns: resource.NewTypedTable[*connEntry](table, TypeWsConn),
		log:   log,
	}
}

// Register adds a connection to the registry and starts the read loop.
func (r *Registry) Register(ctx context.Context, conn *websocket.Conn, bufferSize int, readTimeout time.Duration) uint64 {
	if bufferSize <= 0 {
		bufferSize = 16
	}

	connCtx, cancel := context.WithCancel(ctx)
	entry := &connEntry{
		conn:        conn,
		msgCh:       make(chan wsapi.WsMessage, bufferSize),
		ctx:         connCtx,
		cancel:      cancel,
		readTimeout: readTimeout,
		log:         r.log,
	}

	go entry.readLoop()

	handle := r.conns.Insert(entry)
	return uint64(handle)
}

// get returns a connection entry by ID. Internal use only.
func (r *Registry) get(id uint64) (*connEntry, error) {
	entry, ok := r.conns.Get(resource.Handle(id))
	if !ok {
		return nil, wssvc.NewConnNotFoundError(id)
	}
	if entry.closed.Load() {
		return nil, wssvc.NewConnClosedError(id)
	}
	return entry, nil
}

// GetMessageChan returns the message channel for the given connection ID.
func (r *Registry) GetMessageChan(id uint64) (<-chan wsapi.WsMessage, error) {
	entry, err := r.get(id)
	if err != nil {
		return nil, err
	}
	return entry.msgCh, nil
}

// Close closes a connection with a custom close code.
func (r *Registry) Close(id uint64, code int, reason string) error {
	entry, ok := r.conns.Get(resource.Handle(id))
	if !ok {
		return wssvc.NewConnNotFoundError(id)
	}

	statusCode := websocket.StatusNormalClosure
	if code > 0 {
		statusCode = websocket.StatusCode(code)
	}

	entry.close(statusCode, reason)
	r.conns.Remove(resource.Handle(id))
	return nil
}

// GetRegistry returns a Registry backed by the Table from context.
// Returns nil if no resource table is available.
func GetRegistry(ctx context.Context) *Registry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		table := resource.GetTable(ctx)
		if table == nil {
			return nil
		}
		return NewRegistry(table, nil)
	}

	if val, ok := fc.Get(registryKey); ok {
		return val.(*Registry)
	}

	table := resource.GetTable(ctx)
	if table == nil {
		return nil
	}
	reg := NewRegistry(table, nil)
	if err := fc.Set(registryKey, reg); err != nil {
		reg.log.Debug("failed to cache registry in frame context", zap.Error(err))
	}
	return reg
}

// MustGetRegistry returns a Registry for the context, panics if unavailable.
// Use only during initialization where missing registry is a fatal error.
func MustGetRegistry(ctx context.Context) *Registry {
	registry := GetRegistry(ctx)
	if registry == nil {
		panic("websocket: no resource.Table in context")
	}
	return registry
}
