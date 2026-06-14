// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	config "github.com/wippyai/runtime/api/service/cdc"
	"go.uber.org/zap"
)

type Dispatcher struct {
	ctx     context.Context
	cancel  context.CancelFunc
	jobs    chan dispatchJob
	log     *zap.Logger
	wg      sync.WaitGroup
	workers int
}

type dispatchJob struct {
	ctx      context.Context
	cmd      dispatcher.Command
	receiver dispatcher.ResultReceiver
	tag      uint64
}

type DispatcherOption func(*Dispatcher)

func WithWorkers(n int) DispatcherOption {
	return func(d *Dispatcher) {
		if n > 0 {
			d.workers = n
		}
	}
}

func WithLogger(log *zap.Logger) DispatcherOption {
	return func(d *Dispatcher) {
		d.log = log
	}
}

func NewDispatcher(opts ...DispatcherOption) *Dispatcher {
	d := &Dispatcher{workers: 4}
	for _, opt := range opts {
		opt(d)
	}
	if d.log == nil {
		d.log = zap.NewNop()
	}
	return d
}

func (d *Dispatcher) Start(ctx context.Context) error {
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.jobs = make(chan dispatchJob, d.workers*2)
	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go d.worker()
	}
	return nil
}

func (d *Dispatcher) Stop(context.Context) error {
	if d.cancel != nil {
		d.cancel()
	}
	if d.jobs != nil {
		close(d.jobs)
	}
	d.wg.Wait()
	return nil
}

func (d *Dispatcher) worker() {
	defer d.wg.Done()
	for job := range d.jobs {
		d.execute(job)
	}
}

func (d *Dispatcher) execute(job dispatchJob) {
	if cmd, ok := job.cmd.(config.SubscribeCmd); ok {
		d.executeSubscribe(job.ctx, cmd, job.tag, job.receiver)
	}
}

func (d *Dispatcher) executeSubscribe(ctx context.Context, cmd config.SubscribeCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	streamer := config.GetSourceStreamer(ctx)
	if streamer == nil {
		receiver.CompleteYield(tag, nil, ErrNoSourceStreamer)
		return
	}

	node := relay.GetNode(ctx)
	if node == nil {
		receiver.CompleteYield(tag, nil, ErrNoRelayNode)
		return
	}

	stream, _, err := streamer.Stream(ctx, cmd.Source, cmd.Options)
	if err != nil {
		receiver.CompleteYield(tag, nil, err)
		return
	}

	loopCtx, cancelLoop := context.WithCancel(ctx)
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancelLoop()
			stream.Close()
		})
	}

	go func() {
		for {
			select {
			case change, ok := <-stream.Changes():
				if !ok {
					d.sendTerminal(node, cmd.PID, cmd.Topic, "source close")
					return
				}
				pkg := relay.NewPackage(pid.Zero(), cmd.PID, cmd.Topic, payload.New(change))
				if err := node.Send(pkg); err != nil {
					d.log.Debug("failed to relay cdc change",
						zap.String("source", cmd.Source),
						zap.Error(err))
				}
			case <-loopCtx.Done():
				return
			}
		}
	}()

	receiver.CompleteYield(tag, config.Subscription{Source: cmd.Source, Topic: cmd.Topic, Stop: stop}, nil)
}

func (d *Dispatcher) sendTerminal(node relay.Node, target pid.PID, topic string, reason string) {
	pkg := relay.NewPackage(pid.Zero(), target, topic, payload.NewTerminal())
	if err := node.Send(pkg); err != nil {
		d.log.Debug("failed to send cdc terminal", zap.String("reason", reason), zap.Error(err))
	}
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	select {
	case d.jobs <- dispatchJob{ctx: ctx, cmd: cmd, tag: tag, receiver: receiver}:
	case <-d.ctx.Done():
	}
	return nil
}

func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(config.Subscribe, dispatcher.HandlerFunc(d.handle))
}
