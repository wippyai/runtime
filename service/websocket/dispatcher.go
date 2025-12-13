package websocket

import (
	"context"
	"net/http"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	wssvc "github.com/wippyai/runtime/api/service/websocket"
	wsapi "github.com/wippyai/runtime/api/websocket"

	"github.com/coder/websocket"
	"go.uber.org/zap"
)

// Dispatcher handles WebSocket commands via async worker pool.
type Dispatcher struct {
	workers int
	jobs    chan job
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	log     *zap.Logger
}

type job struct {
	ctx      context.Context
	cmd      dispatcher.Command
	tag      uint64
	receiver dispatcher.ResultReceiver
}

// Option configures a Dispatcher.
type Option func(*Dispatcher)

// WithWorkers sets the number of worker goroutines.
func WithWorkers(n int) Option {
	return func(d *Dispatcher) {
		if n > 0 {
			d.workers = n
		}
	}
}

// WithLogger sets the logger for the dispatcher.
func WithLogger(log *zap.Logger) Option {
	return func(d *Dispatcher) {
		d.log = log
	}
}

// NewDispatcher creates a WebSocket dispatcher with default 4 workers.
func NewDispatcher(opts ...Option) *Dispatcher {
	d := &Dispatcher{workers: 4}
	for _, opt := range opts {
		opt(d)
	}
	if d.log == nil {
		d.log = zap.NewNop()
	}
	return d
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

func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) {
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

func (d *Dispatcher) executeConnect(ctx context.Context, cmd wsapi.WsConnectCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	opts := &websocket.DialOptions{}

	if len(cmd.Headers) > 0 {
		opts.HTTPHeader = make(http.Header)
		for k, v := range cmd.Headers {
			opts.HTTPHeader.Set(k, v)
		}
	}

	if len(cmd.Protocols) > 0 {
		opts.Subprotocols = cmd.Protocols
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

	conn, resp, err := websocket.Dial(dialCtx, cmd.URL, opts)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		receiver.CompleteYield(tag, nil, wssvc.NewDialError(cmd.URL, err))
		return
	}

	readLimit := int64(16 * 1024 * 1024)
	if cmd.ReadLimit > 0 {
		readLimit = cmd.ReadLimit
	}
	conn.SetReadLimit(readLimit)

	registry := MustGetRegistry(ctx)
	id := registry.Register(ctx, conn, cmd.ChannelCapacity, cmd.ReadTimeout)
	receiver.CompleteYield(tag, id, nil)
}

func (d *Dispatcher) executeSend(ctx context.Context, cmd wsapi.WsSendCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, nil, wssvc.NewNoRegistryError())
		return
	}

	entry, err := registry.get(cmd.ConnID)
	if err != nil {
		receiver.CompleteYield(tag, nil, err)
		return
	}

	msgType := websocket.MessageText
	if cmd.MessageType == wsapi.MessageBinary {
		msgType = websocket.MessageBinary
	}

	if err := entry.conn.Write(ctx, msgType, cmd.Data); err != nil {
		receiver.CompleteYield(tag, nil, wssvc.NewSendError(cmd.ConnID, err))
		return
	}
	receiver.CompleteYield(tag, nil, nil)
}

func (d *Dispatcher) executeReceive(ctx context.Context, cmd wsapi.WsReceiveCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, nil, wssvc.NewNoRegistryError())
		return
	}

	msgCh, err := registry.GetMessageChan(cmd.ConnID)
	if err != nil {
		receiver.CompleteYield(tag, nil, err)
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
		receiver.CompleteYield(tag, nil, wssvc.NewReceiveError(cmd.ConnID, ctx.Err()))
	}
}

func (d *Dispatcher) executeClose(ctx context.Context, cmd wsapi.WsCloseCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, nil, wssvc.NewNoRegistryError())
		return
	}

	if err := registry.Close(cmd.ConnID, cmd.Code, cmd.Reason); err != nil {
		receiver.CompleteYield(tag, nil, err)
		return
	}
	receiver.CompleteYield(tag, nil, nil)
}

func (d *Dispatcher) executePing(ctx context.Context, cmd wsapi.WsPingCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, nil, wssvc.NewNoRegistryError())
		return
	}

	entry, err := registry.get(cmd.ConnID)
	if err != nil {
		receiver.CompleteYield(tag, nil, err)
		return
	}

	if err := entry.conn.Ping(ctx); err != nil {
		receiver.CompleteYield(tag, nil, wssvc.NewPingError(cmd.ConnID, err))
		return
	}
	receiver.CompleteYield(tag, nil, nil)
}

func (d *Dispatcher) executeSubscribe(ctx context.Context, cmd wsapi.WsSubscribeCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, nil, wssvc.NewNoRegistryError())
		return
	}

	entry, err := registry.get(cmd.ConnID)
	if err != nil {
		receiver.CompleteYield(tag, nil, err)
		return
	}

	node := relay.GetNode(ctx)
	if node == nil {
		receiver.CompleteYield(tag, nil, wssvc.NewNoRelayNodeError())
		return
	}

	pidVal := cmd.PID
	topic := cmd.Topic
	log := d.log

	go func() {
		for {
			select {
			case msg, ok := <-entry.msgCh:
				if !ok {
					pkg := relay.NewPackage(pid.PID{}, pidVal, topic, payload.NewTerminal())
					if err := node.Send(pkg); err != nil {
						log.Debug("failed to send terminal on channel close",
							zap.Uint64("conn_id", cmd.ConnID),
							zap.Error(err))
					}
					return
				}

				if msg.EOF {
					pkg := relay.NewPackage(pid.PID{}, pidVal, topic, payload.NewTerminal())
					if err := node.Send(pkg); err != nil {
						log.Debug("failed to send terminal on EOF",
							zap.Uint64("conn_id", cmd.ConnID),
							zap.Error(err))
					}
					return
				}

				p := payload.NewPayload(msg.Data, payload.Bytes)
				if msg.MessageType == wsapi.MessageText {
					p = payload.NewPayload(msg.Data, payload.String)
				}

				pkg := relay.NewPackage(pid.PID{}, pidVal, topic, p)
				if err := node.Send(pkg); err != nil {
					log.Debug("failed to relay message",
						zap.Uint64("conn_id", cmd.ConnID),
						zap.Error(err))
				}
			case <-ctx.Done():
				return
			case <-entry.ctx.Done():
				pkg := relay.NewPackage(pid.PID{}, pidVal, topic, payload.NewTerminal())
				if err := node.Send(pkg); err != nil {
					log.Debug("failed to send terminal on connection close",
						zap.Uint64("conn_id", cmd.ConnID),
						zap.Error(err))
				}
				return
			}
		}
	}()

	receiver.CompleteYield(tag, wsapi.WsSubscription{ConnID: cmd.ConnID, Topic: topic}, nil)
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
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
