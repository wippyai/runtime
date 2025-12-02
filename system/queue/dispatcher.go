// Package queue provides queue command handlers for the dispatcher system.
package queue

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	queueapi "github.com/wippyai/runtime/api/dispatcher/queue"
)

// Dispatcher handles queue commands via async worker pool.
type Dispatcher struct {
	workers int
	jobs    chan job
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

type job struct {
	ctx  context.Context
	cmd  dispatcher.Command
	emit dispatcher.Emitter
}

// NewDispatcher creates a queue dispatcher with the specified worker count.
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

func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) {
	select {
	case d.jobs <- job{ctx: ctx, cmd: cmd, emit: emit}:
	case <-d.ctx.Done():
	}
}

func (d *Dispatcher) execute(j job) {
	if c, ok := j.cmd.(*queueapi.QueuePublishCmd); ok {
		err := c.Manager.Publish(j.ctx, c.QueueID, c.Message)
		j.emit.Emit(queueapi.QueuePublishResponse{Error: err}, nil)
	}
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	d.submit(ctx, cmd, emit)
	return nil
}

// RegisterAll registers all queue handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	h := dispatcher.HandlerFunc(d.handle)
	register(queueapi.CmdQueuePublish, h)
}
