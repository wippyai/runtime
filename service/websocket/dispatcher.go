// SPDX-License-Identifier: MPL-2.0

package websocket

import (
	"context"
	"net/http"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	wsapi "github.com/wippyai/runtime/api/service/websocket"

	"github.com/coder/websocket"
	"go.uber.org/zap"
)

// DefaultReadLimit is the maximum message size in bytes (16 MB).
const DefaultReadLimit = 16 * 1024 * 1024

// Dispatcher handles WebSocket commands via async worker pool.
type Dispatcher struct {
	ctx     context.Context
	jobs    chan job
	cancel  context.CancelFunc
	log     *zap.Logger
	wg      sync.WaitGroup
	workers int
}

type job struct {
	ctx      context.Context
	cmd      dispatcher.Command
	receiver dispatcher.ResultReceiver
	tag      uint64
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
	case wsapi.ConnectCmd:
		d.executeConnect(j.ctx, c, j.tag, j.receiver)
	case wsapi.SendCmd:
		d.executeSend(j.ctx, c, j.tag, j.receiver)
	case wsapi.ReceiveCmd:
		d.executeReceive(j.ctx, c, j.tag, j.receiver)
	case wsapi.CloseCmd:
		d.executeClose(j.ctx, c, j.tag, j.receiver)
	case wsapi.PingCmd:
		d.executePing(j.ctx, c, j.tag, j.receiver)
	case wsapi.SubscribeCmd:
		d.executeSubscribe(j.ctx, c, j.tag, j.receiver)
	}
}

func (d *Dispatcher) executeConnect(ctx context.Context, cmd wsapi.ConnectCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, nil, NewNoRegistryError())
		return
	}

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
		if closeErr := resp.Body.Close(); closeErr != nil {
			d.log.Debug("failed to close response body", zap.Error(closeErr))
		}
	}
	if err != nil {
		receiver.CompleteYield(tag, nil, NewDialError(cmd.URL, err))
		return
	}

	readLimit := int64(DefaultReadLimit)
	if cmd.ReadLimit > 0 {
		readLimit = cmd.ReadLimit
	}
	conn.SetReadLimit(readLimit)

	id := registry.Register(ctx, conn, cmd.ChannelCapacity, cmd.ReadTimeout)
	receiver.CompleteYield(tag, id, nil)
}

func (d *Dispatcher) executeSend(ctx context.Context, cmd wsapi.SendCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, nil, NewNoRegistryError())
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
		receiver.CompleteYield(tag, nil, NewSendError(cmd.ConnID, err))
		return
	}
	receiver.CompleteYield(tag, nil, nil)
}

func (d *Dispatcher) executeReceive(ctx context.Context, cmd wsapi.ReceiveCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, nil, NewNoRegistryError())
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
			receiver.CompleteYield(tag, wsapi.Message{EOF: true}, nil)
			return
		}
		receiver.CompleteYield(tag, msg, nil)
	case <-ctx.Done():
		receiver.CompleteYield(tag, nil, NewReceiveError(cmd.ConnID, ctx.Err()))
	}
}

func (d *Dispatcher) executeClose(ctx context.Context, cmd wsapi.CloseCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, nil, NewNoRegistryError())
		return
	}

	if err := registry.Close(cmd.ConnID, cmd.Code, cmd.Reason); err != nil {
		receiver.CompleteYield(tag, nil, err)
		return
	}
	receiver.CompleteYield(tag, nil, nil)
}

func (d *Dispatcher) executePing(ctx context.Context, cmd wsapi.PingCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, nil, NewNoRegistryError())
		return
	}

	entry, err := registry.get(cmd.ConnID)
	if err != nil {
		receiver.CompleteYield(tag, nil, err)
		return
	}

	if err := entry.conn.Ping(ctx); err != nil {
		receiver.CompleteYield(tag, nil, NewPingError(cmd.ConnID, err))
		return
	}
	receiver.CompleteYield(tag, nil, nil)
}

func (d *Dispatcher) executeSubscribe(ctx context.Context, cmd wsapi.SubscribeCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	registry := GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, nil, NewNoRegistryError())
		return
	}

	entry, err := registry.get(cmd.ConnID)
	if err != nil {
		receiver.CompleteYield(tag, nil, err)
		return
	}

	node := relay.GetNode(ctx)
	if node == nil {
		receiver.CompleteYield(tag, nil, NewNoRelayNodeError())
		return
	}

	pidVal := cmd.PID
	topic := cmd.Topic
	log := d.log
	connID := cmd.ConnID

	// stop is the producer-stop handle wired back into the process through
	// SetSubscriptionCleanup. closeChannel / drain / Abort / overflow call it
	// when the subscription is reclaimed: it cancels the relay goroutine and
	// tears down the connection so the registry read loop (entry.readLoop)
	// also exits. No goroutine leak when the process drains, the consumer
	// overflows, or the connection is closed from Lua.
	loopCtx, cancelLoop := context.WithCancel(ctx)
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancelLoop()
			registry.dropForStop(connID)
		})
	}

	// sendTerminal relays a terminal so deliverMessage reclaims the
	// subscription on a connection-driven close (remote close / EOF / read
	// error). It is suppressed when stop already fired: a reclaim-triggered
	// stop (closeChannel / drain / overflow) removes the subscription on the
	// step thread, so a terminal to the now-unmatched unique topic is
	// redundant and must not be emitted.
	sendTerminal := func(reason string) {
		if entry.reclaimedByProcess.Load() {
			return
		}
		select {
		case <-loopCtx.Done():
			return
		default:
		}
		pkg := relay.NewPackage(pid.Zero(), pidVal, topic, payload.NewTerminal())
		if err := node.Send(pkg); err != nil {
			log.Debug("failed to send terminal: "+reason,
				zap.Uint64("conn_id", connID),
				zap.Error(err))
		}
	}

	go func() {
		for {
			select {
			case msg, ok := <-entry.msgCh:
				if !ok {
					sendTerminal("channel close")
					return
				}

				if msg.EOF {
					sendTerminal("EOF")
					return
				}

				p := payload.NewPayload(msg.Data, payload.Bytes)
				if msg.MessageType == wsapi.MessageText {
					p = payload.NewPayload(msg.Data, payload.String)
				}

				pkg := relay.NewPackage(pid.Zero(), pidVal, topic, p)
				if err := node.Send(pkg); err != nil {
					log.Debug("failed to relay message",
						zap.Uint64("conn_id", connID),
						zap.Error(err))
				}
			case <-loopCtx.Done():
				// Reclaim-triggered stop. Subscription already gone; emit no
				// terminal to the now-unmatched topic.
				return
			case <-entry.ctx.Done():
				sendTerminal("connection close")
				return
			}
		}
	}()

	receiver.CompleteYield(tag, wsapi.Subscription{ConnID: cmd.ConnID, Topic: topic, Stop: stop}, nil)
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	d.submit(ctx, cmd, tag, receiver)
	return nil
}

// RegisterAll registers all WebSocket handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	h := dispatcher.HandlerFunc(d.handle)
	register(wsapi.Connect, h)
	register(wsapi.Send, h)
	register(wsapi.Receive, h)
	register(wsapi.Close, h)
	register(wsapi.Ping, h)
	register(wsapi.Subscribe, h)
}
