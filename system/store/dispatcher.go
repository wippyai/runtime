// Package store provides store command handlers for the dispatcher system.
// Supports both blocking (for testing) and async (for production) execution modes.
package store

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	storeapi "github.com/wippyai/runtime/api/dispatcher/store"
)

// job represents a unit of work for the async dispatcher.
type job struct {
	ctx  context.Context
	cmd  dispatcher.Command
	emit dispatcher.EmitFunc
}

// Dispatcher handles store commands with configurable execution mode.
type Dispatcher struct {
	workers int
	jobs    chan job
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.RWMutex
	stopped bool
}

// Config holds dispatcher configuration.
type Config struct {
	// Workers is the number of worker goroutines for async mode.
	// If 0, dispatcher runs in blocking mode (synchronous execution).
	Workers int
}

// NewDispatcher creates a new store dispatcher with the given configuration.
func NewDispatcher(cfg Config) *Dispatcher {
	return &Dispatcher{
		workers: cfg.Workers,
	}
}

// NewBlockingDispatcher creates a dispatcher that executes synchronously.
// Use for testing or when async execution is not needed.
func NewBlockingDispatcher() *Dispatcher {
	return &Dispatcher{workers: 0}
}

// NewAsyncDispatcher creates a dispatcher with a worker pool.
// Use for production to avoid blocking the scheduler.
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

	// Mark as stopped under lock to prevent new submissions
	d.mu.Lock()
	d.stopped = true
	d.mu.Unlock()

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
// Returns false if dispatcher is stopped.
func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) bool {
	d.mu.RLock()
	if d.stopped {
		d.mu.RUnlock()
		return false
	}

	// Hold lock while sending to prevent close during send
	select {
	case d.jobs <- job{ctx: ctx, cmd: cmd, emit: emit}:
		d.mu.RUnlock()
		return true
	case <-d.ctx.Done():
		d.mu.RUnlock()
		return false
	}
}

// isAsync returns true if dispatcher is in async mode.
func (d *Dispatcher) isAsync() bool {
	return d.workers > 0 && d.jobs != nil
}

// execute runs the store operation and emits the result.
func execute(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) {
	switch c := cmd.(type) {
	case *storeapi.StoreGetCmd:
		value, err := c.Store.Get(ctx, c.Key)
		emit(storeapi.StoreGetResponse{Value: value, Error: err})

	case *storeapi.StoreSetCmd:
		err := c.Store.Set(ctx, c.Entry)
		emit(storeapi.StoreSetResponse{Error: err})

	case *storeapi.StoreDeleteCmd:
		err := c.Store.Delete(ctx, c.Key)
		emit(storeapi.StoreDeleteResponse{Error: err})

	case *storeapi.StoreHasCmd:
		exists, err := c.Store.Has(ctx, c.Key)
		emit(storeapi.StoreHasResponse{Exists: exists, Error: err})
	}
}

// handler routes commands through the dispatcher.
type handler struct {
	d *Dispatcher
}

func (h *handler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		if !h.d.submit(ctx, cmd, emit) {
			// Dispatcher stopped, execute synchronously as fallback
			execute(ctx, cmd, emit)
		}
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// RegisterAll registers all store handlers with the given registry function.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	h := &handler{d: d}
	register(storeapi.CmdStoreGet, h)
	register(storeapi.CmdStoreSet, h)
	register(storeapi.CmdStoreDelete, h)
	register(storeapi.CmdStoreHas, h)
}
