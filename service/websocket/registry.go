// SPDX-License-Identifier: MPL-2.0

// Package websocket provides WebSocket command handlers for the dispatcher system.
package websocket

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/runtime/resource"
	wsapi "github.com/wippyai/runtime/api/service/websocket"

	"github.com/coder/websocket"
	"go.uber.org/zap"
)

// TypeWsConn is the type ID for WebSocket connections in the resource table.
const TypeWsConn uint32 = 0x20

// registryKey caches websocket Registry in FrameContext for request/process lifetime.
var registryKey = &ctxapi.Key{Name: "websocket.registry", Inherit: false}

// connEntry holds an active WebSocket connection with its message channel.
type connEntry struct {
	ctx         context.Context
	conn        *websocket.Conn
	msgCh       chan wsapi.Message
	cancel      context.CancelFunc
	log         *zap.Logger
	readTimeout time.Duration
	closeOnce   sync.Once
	closed      atomic.Bool
	// reclaimedByProcess marks a process-initiated teardown (conn:close or a
	// subscription reclaim via close/drain/overflow). The relay read loop reads
	// it to suppress the terminal frame: the process already removed the
	// subscription on its step thread, so a terminal to the now-unmatched topic
	// must not be relayed. A connection-originated close (remote / EOF / read
	// error) leaves it false so the terminal is delivered and reclaims the
	// subscription.
	reclaimedByProcess atomic.Bool
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
				case e.msgCh <- wsapi.Message{EOF: true}:
				case <-e.ctx.Done():
				}
			}
			return
		}

		mt := wsapi.MessageText
		if msgType == websocket.MessageBinary {
			mt = wsapi.MessageBinary
		}

		msg := wsapi.Message{
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
		msgCh:       make(chan wsapi.Message, bufferSize),
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
		return nil, NewConnNotFoundError(id)
	}
	if entry.closed.Load() {
		return nil, NewConnClosedError(id)
	}
	return entry, nil
}

// GetMessageChan returns the message channel for the given connection ID.
func (r *Registry) GetMessageChan(id uint64) (<-chan wsapi.Message, error) {
	entry, err := r.get(id)
	if err != nil {
		return nil, err
	}
	return entry.msgCh, nil
}

// dropForStop signals a connection to tear down when its subscription is
// reclaimed (close / drain / overflow). It only cancels entry.ctx, which is
// nonblocking and safe to call from the process step thread: the registry read
// loop observes the cancellation, then closes the socket and the message
// channel off the step thread, and the dispatcher relay goroutine exits on the
// closed channel. The handle slot is freed when the resource store is released
// at process teardown. Idempotent: a missing handle and the entry's cancel are
// both no-ops on repeat.
func (r *Registry) dropForStop(id uint64) {
	entry, ok := r.conns.Get(resource.Handle(id))
	if !ok {
		return
	}
	entry.reclaimedByProcess.Store(true)
	entry.closed.Store(true)
	entry.cancel()
}

// Close closes a connection with a custom close code.
func (r *Registry) Close(id uint64, code int, reason string) error {
	entry, ok := r.conns.Get(resource.Handle(id))
	if !ok {
		return NewConnNotFoundError(id)
	}

	statusCode := websocket.StatusNormalClosure
	if code > 0 {
		statusCode = websocket.StatusCode(code)
	}

	// Process-initiated close: the Lua conn:close() path reclaims the
	// subscription on its step thread, so suppress the relay terminal.
	entry.reclaimedByProcess.Store(true)
	entry.close(statusCode, reason)
	r.conns.Remove(resource.Handle(id))
	return nil
}

// GetRegistry returns a Registry backed by the Table from context.
// Returns nil if no resource table is available.
func GetRegistry(ctx context.Context) *Registry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc != nil {
		if cached, ok := fc.Get(registryKey); ok {
			if reg, ok := cached.(*Registry); ok {
				return reg
			}
		}
	}

	table := resource.GetTable(ctx)
	if table == nil {
		return nil
	}

	reg := NewRegistry(table, nil)
	if fc != nil {
		// Frame may already be sealed; caching is best-effort only.
		_ = fc.Set(registryKey, reg)
	}
	return reg
}

// MustGetRegistry returns a Registry for the context, panics if unavailable.
// Use only during initialization where missing registry is a fatal error.
func MustGetRegistry(ctx context.Context) *Registry {
	registry := GetRegistry(ctx)
	if registry == nil {
		// Logical invariant: registry must exist in context at this call site.
		panic("websocket: no resource.Table in context")
	}
	return registry
}
