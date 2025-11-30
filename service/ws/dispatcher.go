// Package ws provides WebSocket command handlers for the dispatcher system.
// Supports both blocking (for testing) and async (for production) execution modes.
package ws

import (
	"context"
	"errors"
	"net/http"
	"sync"

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

// job represents a unit of work for the async dispatcher.
type job struct {
	ctx  context.Context
	cmd  dispatcher.Command
	emit dispatcher.EmitFunc
}

// Dispatcher handles WebSocket commands with configurable execution mode.
type Dispatcher struct {
	workers int
	jobs    chan job
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

// Config holds dispatcher configuration.
type Config struct {
	// Workers is the number of worker goroutines for async mode.
	// If 0, dispatcher runs in blocking mode (synchronous execution).
	Workers int
}

// NewDispatcher creates a new WebSocket dispatcher with the given configuration.
func NewDispatcher(cfg Config) *Dispatcher {
	return &Dispatcher{
		workers: cfg.Workers,
	}
}

// NewBlockingDispatcher creates a dispatcher that executes synchronously.
func NewBlockingDispatcher() *Dispatcher {
	return &Dispatcher{workers: 0}
}

// NewAsyncDispatcher creates a dispatcher with a worker pool.
func NewAsyncDispatcher(workers int) *Dispatcher {
	if workers <= 0 {
		workers = 4
	}
	return &Dispatcher{workers: workers}
}

// Start initializes the dispatcher. For async mode, starts worker goroutines.
func (d *Dispatcher) Start(ctx context.Context) error {
	if d.workers <= 0 {
		return nil
	}

	d.ctx, d.cancel = context.WithCancel(ctx)
	d.jobs = make(chan job, d.workers*2)

	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go d.worker()
	}

	return nil
}

// Stop shuts down the dispatcher and waits for workers to finish.
func (d *Dispatcher) Stop(_ context.Context) error {
	if d.workers <= 0 {
		return nil
	}

	d.cancel()
	close(d.jobs)
	d.wg.Wait()
	return nil
}

// worker processes jobs from the queue.
func (d *Dispatcher) worker() {
	defer d.wg.Done()

	for j := range d.jobs {
		execute(j.ctx, j.cmd, j.emit)
	}
}

// submit sends a job to the worker pool.
func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) {
	select {
	case d.jobs <- job{ctx: ctx, cmd: cmd, emit: emit}:
	case <-d.ctx.Done():
	}
}

// isAsync returns true if dispatcher is in async mode.
func (d *Dispatcher) isAsync() bool {
	return d.workers > 0 && d.jobs != nil
}

// execute runs the WebSocket operation and emits the result.
func execute(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) {
	switch c := cmd.(type) {
	case wsapi.WsConnectCmd:
		executeConnect(ctx, c, emit)
	case wsapi.WsSendCmd:
		executeSend(ctx, c, emit)
	case wsapi.WsReceiveCmd:
		executeReceive(ctx, c, emit)
	case wsapi.WsCloseCmd:
		executeClose(ctx, c, emit)
	case wsapi.WsPingCmd:
		executePing(ctx, c, emit)
	case wsapi.WsSubscribeCmd:
		executeSubscribe(ctx, c, emit)
	}
}

func executeConnect(ctx context.Context, cmd wsapi.WsConnectCmd, emit dispatcher.EmitFunc) {
	opts := &websocket.DialOptions{}

	if len(cmd.Headers) > 0 {
		opts.HTTPHeader = make(http.Header)
		for k, v := range cmd.Headers {
			opts.HTTPHeader.Set(k, v)
		}
	}

	switch cmd.CompressionMode {
	case wsapi.CompressionContextTakeover:
		opts.CompressionMode = websocket.CompressionContextTakeover
	case wsapi.CompressionNoContext:
		opts.CompressionMode = websocket.CompressionNoContextTakeover
	default:
		opts.CompressionMode = websocket.CompressionDisabled
	}
	if cmd.CompressionThreshold > 0 {
		opts.CompressionThreshold = cmd.CompressionThreshold
	}

	dialCtx := ctx
	if cmd.DialTimeout > 0 {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeout(ctx, cmd.DialTimeout)
		defer cancel()
	}

	conn, _, err := websocket.Dial(dialCtx, cmd.URL, opts)
	if err != nil {
		return
	}

	if cmd.ReadLimit > 0 {
		conn.SetReadLimit(cmd.ReadLimit)
	}

	registry := GetOrCreateWsRegistry(ctx)
	id := registry.Register(conn)
	emit(id)
}

func executeSend(ctx context.Context, cmd wsapi.WsSendCmd, emit dispatcher.EmitFunc) {
	registry := GetWsRegistry(ctx)
	if registry == nil {
		return
	}

	entry, err := registry.Get(cmd.ConnID)
	if err != nil {
		return
	}

	msgType := websocket.MessageText
	if cmd.MessageType == wsapi.MessageBinary {
		msgType = websocket.MessageBinary
	}

	if err := entry.conn.Write(ctx, msgType, cmd.Data); err != nil {
		return
	}
	emit(nil)
}

func executeReceive(ctx context.Context, cmd wsapi.WsReceiveCmd, emit dispatcher.EmitFunc) {
	registry := GetWsRegistry(ctx)
	if registry == nil {
		return
	}

	entry, err := registry.Get(cmd.ConnID)
	if err != nil {
		return
	}

	msgType, data, err := entry.conn.Read(ctx)
	if err != nil {
		closeStatus := websocket.CloseStatus(err)
		if closeStatus >= 0 {
			emit(wsapi.WsMessage{EOF: true})
		}
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

func executeClose(ctx context.Context, cmd wsapi.WsCloseCmd, emit dispatcher.EmitFunc) {
	registry := GetWsRegistry(ctx)
	if registry == nil {
		return
	}

	if err := registry.Close(cmd.ConnID, cmd.Code, cmd.Reason); err != nil {
		return
	}
	emit(nil)
}

func executePing(ctx context.Context, cmd wsapi.WsPingCmd, emit dispatcher.EmitFunc) {
	registry := GetWsRegistry(ctx)
	if registry == nil {
		return
	}

	entry, err := registry.Get(cmd.ConnID)
	if err != nil {
		return
	}

	if err := entry.conn.Ping(ctx); err != nil {
		return
	}
	emit(nil)
}

func executeSubscribe(ctx context.Context, cmd wsapi.WsSubscribeCmd, emit dispatcher.EmitFunc) {
	registry := GetWsRegistry(ctx)
	if registry == nil {
		return
	}

	entry, err := registry.Get(cmd.ConnID)
	if err != nil {
		return
	}

	emit(wsapi.WsSubscription(cmd))

	// Subscribe is special - it needs to run a loop
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
}

// ConnectHandler handles WebSocket connect commands.
type ConnectHandler struct {
	d *Dispatcher
}

func (h *ConnectHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// SendHandler handles WebSocket send commands.
type SendHandler struct {
	d *Dispatcher
}

func (h *SendHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// ReceiveHandler handles WebSocket receive commands.
type ReceiveHandler struct {
	d *Dispatcher
}

func (h *ReceiveHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// CloseHandler handles WebSocket close commands.
type CloseHandler struct {
	d *Dispatcher
}

func (h *CloseHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// PingHandler handles WebSocket ping commands.
type PingHandler struct {
	d *Dispatcher
}

func (h *PingHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// SubscribeHandler handles WebSocket subscribe commands.
type SubscribeHandler struct {
	d *Dispatcher
}

func (h *SubscribeHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// RegisterAll registers all WebSocket handlers with the given registry function.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(wsapi.CmdWsConnect, &ConnectHandler{d: d})
	register(wsapi.CmdWsSend, &SendHandler{d: d})
	register(wsapi.CmdWsReceive, &ReceiveHandler{d: d})
	register(wsapi.CmdWsClose, &CloseHandler{d: d})
	register(wsapi.CmdWsPing, &PingHandler{d: d})
	register(wsapi.CmdWsSubscribe, &SubscribeHandler{d: d})
}

// Service is an alias for Dispatcher for backward compatibility.
type Service = Dispatcher

// NewService creates a blocking dispatcher for backward compatibility.
func NewService() *Dispatcher {
	return NewBlockingDispatcher()
}
