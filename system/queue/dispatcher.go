// Package queue provides queue command handlers for the dispatcher system.
// Supports both blocking (for testing) and async (for production) execution modes.
package queue

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	queueapi "github.com/wippyai/runtime/api/dispatcher/queue"
)

// job represents a unit of work for the async dispatcher.
type job struct {
	ctx  context.Context
	cmd  dispatcher.Command
	emit dispatcher.EmitFunc
}

// Dispatcher handles queue commands with configurable execution mode.
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

// NewDispatcher creates a new queue dispatcher with the given configuration.
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

// execute runs the queue operation and emits the result.
func execute(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) {
	switch c := cmd.(type) {
	case *queueapi.QueuePublishCmd:
		err := c.Manager.Publish(ctx, c.QueueID, c.Message)
		emit(queueapi.QueuePublishResponse{Error: err})
	}
}

// PublishHandler handles queue publish commands.
type PublishHandler struct {
	d *Dispatcher
}

func (h *PublishHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// RegisterAll registers all queue handlers with the given registry function.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(queueapi.CmdQueuePublish, &PublishHandler{d: d})
}

// Service is an alias for Dispatcher for backward compatibility.
type Service = Dispatcher

// NewService creates a blocking dispatcher for backward compatibility.
func NewService() *Dispatcher {
	return NewBlockingDispatcher()
}
