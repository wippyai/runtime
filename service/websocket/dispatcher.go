// Package websocket provides WebSocket command handlers for the dispatcher system.
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
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime/resource"

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
	msgCh     chan wsapi.WsMessage
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
	})
}

// readLoop continuously reads messages from the websocket and sends to channel.
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

	entry.Close(statusCode, reason)
	r.conns.Remove(resource.Handle(id))
	return nil
}

// GetRegistry returns a Registry backed by the Table from context.
func GetRegistry(ctx context.Context) *Registry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		table := resource.GetTable(ctx)
		if table == nil {
			return nil
		}
		return NewRegistry(table)
	}

	if val, ok := fc.Get(registryKey); ok {
		return val.(*Registry)
	}

	table := resource.GetTable(ctx)
	if table == nil {
		return nil
	}
	reg := NewRegistry(table)
	_ = fc.Set(registryKey, reg)
	return reg
}

// GetOrCreateRegistry returns a Registry for the context.
func GetOrCreateRegistry(ctx context.Context) *Registry {
	registry := GetRegistry(ctx)
	if registry == nil {
		panic("websocket: no resource.Store in context - engine must set it during initialization")
	}
	return registry
}

// Dispatcher handles WebSocket commands via async worker pool.
type Dispatcher struct {
	workers int
	jobs    chan job
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

type job struct {
	ctx      context.Context
	cmd      dispatcher.Command
	tag      any
	receiver process.ResultReceiver
}

// NewDispatcher creates a WebSocket dispatcher with the specified worker count.
func NewDispatcher(workers int) *Dispatcher {
	if workers <= 0 {
		workers = 4
	}
	return &Dispatcher{workers: workers}
}

// Start initializes the worker pool.
func (d *Dispatcher) Start(ctx context.Context) error {
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.jobs = make(chan job, d.workers*2)

	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go d.worker()
	}
	return nil
}

// Stop shuts down the dispatcher and drains pending jobs.
func (d *Dispatcher) Stop(_ context.Context) error {
	d.cancel()
	close(d.jobs)
	d.wg.Wait()
	return nil
}

func (d *Dispatcher) worker() {
	defer d.wg.Done()
	for j := range d.jobs {
		d.execute(j)
	}
}

func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, tag any, receiver process.ResultReceiver) {
	select {
	case d.jobs <- job{ctx: ctx, cmd: cmd, tag: tag, receiver: receiver}:
	case <-d.ctx.Done():
	}
}

func (d *Dispatcher) execute(j job) {
	switch c := j.cmd.(type) {
	case wsapi.WsConnectCmd:
		d.executeConnect(j.ctx, c, j.tag, j.receiver)
	case wsapi.WsSendCmd:
		d.executeSend(j.ctx, c, j.tag, j.receiver)
	case wsapi.WsReceiveCmd:
		d.executeReceive(j.ctx, c, j.tag, j.receiver)
	case wsapi.WsCloseCmd:
		d.executeClose(j.ctx, c, j.tag, j.receiver)
	case wsapi.WsPingCmd:
		d.executePing(j.ctx, c, j.tag, j.receiver)
	case wsapi.WsSubscribeCmd:
		d.executeSubscribe(j.ctx, c, j.tag, j.receiver)
	}
}

func (d *Dispatcher) executeConnect(ctx context.Context, cmd wsapi.WsConnectCmd, tag any, receiver process.ResultReceiver) {
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
	id := registry.Register(ctx, conn, 16)
	receiver.CompleteYield(tag, id, nil)
}

func (d *Dispatcher) executeSend(ctx context.Context, cmd wsapi.WsSendCmd, tag any, receiver process.ResultReceiver) {
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
	receiver.CompleteYield(tag, nil, nil)
}

func (d *Dispatcher) executeReceive(ctx context.Context, cmd wsapi.WsReceiveCmd, tag any, receiver process.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		return
	}

	msgCh, err := registry.GetMessageChan(cmd.ConnID)
	if err != nil {
		return
	}

	select {
	case msg, ok := <-msgCh:
		if !ok {
			receiver.CompleteYield(tag, wsapi.WsMessage{EOF: true}, nil)
			return
		}
		receiver.CompleteYield(tag, msg, nil)
	case <-ctx.Done():
		return
	}
}

func (d *Dispatcher) executeClose(ctx context.Context, cmd wsapi.WsCloseCmd, tag any, receiver process.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		return
	}

	if err := registry.Close(cmd.ConnID, cmd.Code, cmd.Reason); err != nil {
		return
	}
	receiver.CompleteYield(tag, nil, nil)
}

func (d *Dispatcher) executePing(ctx context.Context, cmd wsapi.WsPingCmd, tag any, receiver process.ResultReceiver) {
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
	receiver.CompleteYield(tag, nil, nil)
}

func (d *Dispatcher) executeSubscribe(ctx context.Context, cmd wsapi.WsSubscribeCmd, tag any, receiver process.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		return
	}

	entry, err := registry.Get(cmd.ConnID)
	if err != nil {
		return
	}

	node := relay.GetNode(ctx)
	if node == nil {
		return
	}

	// Use PID and topic from command
	pid := cmd.PID
	topic := relay.Topic(cmd.Topic)

	go func() {
		for {
			select {
			case msg, ok := <-entry.msgCh:
				if !ok {
					// Channel closed - send terminal to close subscriber channel
					pkg := relay.NewPackage(relay.PID{}, pid, topic, payload.NewTerminal())
					_ = node.Send(pkg)
					return
				}

				if msg.EOF {
					// EOF received - send terminal to close subscriber channel
					pkg := relay.NewPackage(relay.PID{}, pid, topic, payload.NewTerminal())
					_ = node.Send(pkg)
					return
				}

				p := payload.NewPayload(msg.Data, payload.Bytes)
				if msg.MessageType == wsapi.MessageText {
					p = payload.NewPayload(msg.Data, payload.String)
				}

				pkg := relay.NewPackage(relay.PID{}, pid, topic, p)
				_ = node.Send(pkg)
			case <-ctx.Done():
				// Request context canceled - process finished, stop forwarding
				return
			case <-entry.ctx.Done():
				// Connection closed - send terminal to close subscriber channel
				pkg := relay.NewPackage(relay.PID{}, pid, topic, payload.NewTerminal())
				_ = node.Send(pkg)
				return
			}
		}
	}()

	receiver.CompleteYield(tag, wsapi.WsSubscription{ConnID: cmd.ConnID, Topic: string(topic)}, nil)
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, tag any, receiver process.ResultReceiver) error {
	d.submit(ctx, cmd, tag, receiver)
	return nil
}

// RegisterAll registers all WebSocket handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	h := dispatcher.HandlerFunc(d.handle)
	register(wsapi.CmdWsConnect, h)
	register(wsapi.CmdWsSend, h)
	register(wsapi.CmdWsReceive, h)
	register(wsapi.CmdWsClose, h)
	register(wsapi.CmdWsPing, h)
	register(wsapi.CmdWsSubscribe, h)
}
