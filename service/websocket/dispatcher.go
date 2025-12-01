// Package websocket provides WebSocket command handlers for the dispatcher system.
// Uses background read goroutines for non-blocking message delivery via channels.
package websocket

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	wsapi "github.com/wippyai/runtime/api/dispatcher/ws"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/runtime"

	"github.com/coder/websocket"
)

// TypeWsConn is the type ID for WebSocket connections in the resource table.
const TypeWsConn uint32 = 0x20

// TopicWsMessage is the relay topic for WebSocket messages delivered via subscription.
const TopicWsMessage relay.Topic = "ws.message"

// registryKey is the FrameContext key for caching the websocket Registry.
var registryKey = &ctxapi.Key{Name: "websocket.registry", Inherit: false}

// Errors
var (
	ErrConnNotFound = errors.New("websocket connection not found")
	ErrConnClosed   = errors.New("websocket connection closed")
)

// connEntry holds an active WebSocket connection with its message channel.
type connEntry struct {
	conn      *websocket.Conn
	msgCh     chan wsapi.WsMessage // Channel for received messages
	ctx       context.Context
	cancel    context.CancelFunc
	closed    atomic.Bool
	closeOnce sync.Once
}

// Drop implements resource.Dropper for automatic cleanup.
func (e *connEntry) Drop() {
	e.Close(websocket.StatusGoingAway, "resource dropped")
}

// Close closes the connection and stops the read loop.
func (e *connEntry) Close(code websocket.StatusCode, reason string) {
	e.closeOnce.Do(func() {
		e.closed.Store(true)
		e.cancel()
		_ = e.conn.Close(code, reason)
		// Channel is closed by readLoop defer, not here
		// This prevents race between cancel() and close()
	})
}

// readLoop continuously reads messages from the websocket and sends to channel.
// Runs until connection closes or context is cancelled.
func (e *connEntry) readLoop() {
	defer func() {
		close(e.msgCh)
		e.Close(websocket.StatusGoingAway, "read loop ended")
	}()

	for {
		select {
		case <-e.ctx.Done():
			return
		default:
		}

		msgType, data, err := e.conn.Read(e.ctx)
		if err != nil {
			closeStatus := websocket.CloseStatus(err)
			if closeStatus >= 0 || e.ctx.Err() != nil {
				// Send EOF message before closing - block until delivered or context cancelled
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

// Registry manages active WebSocket connections using the resource table.
type Registry struct {
	conns *resource.TypedTable[*connEntry]
}

// NewRegistry creates a WebSocket registry backed by the given table.
func NewRegistry(table *resource.Table) *Registry {
	return &Registry{
		conns: resource.NewTypedTable[*connEntry](table, TypeWsConn),
	}
}

// Register adds a connection to the registry and starts the read loop.
func (r *Registry) Register(ctx context.Context, conn *websocket.Conn, bufferSize int) uint64 {
	if bufferSize <= 0 {
		bufferSize = 16
	}

	connCtx, cancel := context.WithCancel(ctx)
	entry := &connEntry{
		conn:   conn,
		msgCh:  make(chan wsapi.WsMessage, bufferSize),
		ctx:    connCtx,
		cancel: cancel,
	}

	// Start background read loop
	go entry.readLoop()

	handle := r.conns.Insert(entry)
	return uint64(handle)
}

// Get returns a connection entry by ID.
func (r *Registry) Get(id uint64) (*connEntry, error) {
	entry, ok := r.conns.Get(resource.Handle(id))
	if !ok {
		return nil, ErrConnNotFound
	}
	if entry.closed.Load() {
		return nil, ErrConnClosed
	}
	return entry, nil
}

// GetMessageChan returns the message channel for the given connection ID.
// This channel can be used in select{} for non-blocking message receipt.
func (r *Registry) GetMessageChan(id uint64) (<-chan wsapi.WsMessage, error) {
	entry, err := r.Get(id)
	if err != nil {
		return nil, err
	}
	return entry.msgCh, nil
}

// Close closes a connection with a custom close code.
func (r *Registry) Close(id uint64, code int, reason string) error {
	entry, ok := r.conns.Get(resource.Handle(id))
	if !ok {
		return ErrConnNotFound
	}

	statusCode := websocket.StatusNormalClosure
	if code > 0 {
		statusCode = websocket.StatusCode(code)
	}

	// Close with custom code before Remove() calls Drop()
	entry.Close(statusCode, reason)

	// Remove from table (Drop() is no-op due to closeOnce)
	r.conns.Remove(resource.Handle(id))
	return nil
}

// GetRegistry returns a Registry backed by the Table from context.
// Returns nil if no Table is available in the context.
// The Registry is cached in the FrameContext for efficiency.
func GetRegistry(ctx context.Context) *Registry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		// Fallback for non-frame contexts
		table := resource.GetTable(ctx)
		if table == nil {
			return nil
		}
		return NewRegistry(table)
	}

	// Check cache first
	if val, ok := fc.Get(registryKey); ok {
		return val.(*Registry)
	}

	// Create and cache
	table := resource.GetTable(ctx)
	if table == nil {
		return nil
	}
	reg := NewRegistry(table)
	_ = fc.Set(registryKey, reg)
	return reg
}

// GetOrCreateRegistry returns a Registry for the context.
// Panics if no Store is available - engines must set resource.Store during initialization.
func GetOrCreateRegistry(ctx context.Context) *Registry {
	registry := GetRegistry(ctx)
	if registry == nil {
		panic("websocket: no resource.Store in context - engine must set it during initialization")
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

	registry := GetOrCreateRegistry(ctx)
	id := registry.Register(ctx, conn, 16) // Buffer 16 messages
	emit(id)
}

func executeSend(ctx context.Context, cmd wsapi.WsSendCmd, emit dispatcher.EmitFunc) {
	registry := GetRegistry(ctx)
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
	registry := GetRegistry(ctx)
	if registry == nil {
		return
	}

	msgCh, err := registry.GetMessageChan(cmd.ConnID)
	if err != nil {
		return
	}

	// Non-blocking receive from channel
	select {
	case msg, ok := <-msgCh:
		if !ok {
			emit(wsapi.WsMessage{EOF: true})
			return
		}
		emit(msg)
	case <-ctx.Done():
		return
	}
}

func executeClose(ctx context.Context, cmd wsapi.WsCloseCmd, emit dispatcher.EmitFunc) {
	registry := GetRegistry(ctx)
	if registry == nil {
		return
	}

	if err := registry.Close(cmd.ConnID, cmd.Code, cmd.Reason); err != nil {
		return
	}
	emit(nil)
}

func executePing(ctx context.Context, cmd wsapi.WsPingCmd, emit dispatcher.EmitFunc) {
	registry := GetRegistry(ctx)
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
	registry := GetRegistry(ctx)
	if registry == nil {
		return
	}

	entry, err := registry.Get(cmd.ConnID)
	if err != nil {
		return
	}

	pid, ok := runtime.GetFramePID(ctx)
	if !ok {
		return
	}

	node := relay.GetNode(ctx)
	if node == nil {
		return
	}

	// Spawn goroutine that reads from channel and sends to process via relay.
	// Exits when: msgCh closes, EOF received, or connection context cancelled.
	go func() {
		for {
			select {
			case msg, ok := <-entry.msgCh:
				if !ok {
					return
				}

				p := payload.NewPayload(msg.Data, payload.Bytes)
				if msg.MessageType == wsapi.MessageText {
					p = payload.NewPayload(msg.Data, payload.String)
				}
				if msg.EOF {
					p = payload.NewPayload(nil, payload.Golang)
				}

				pkg := relay.NewPackage(relay.PID{}, pid, TopicWsMessage, p)
				_ = node.Send(pkg)

				if msg.EOF {
					return
				}
			case <-entry.ctx.Done():
				return
			}
		}
	}()

	emit(wsapi.WsSubscription{ConnID: cmd.ConnID})
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
