// Package store provides store command handlers for the dispatcher system.
package store

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
	storeapi "github.com/wippyai/runtime/api/dispatcher/store"
)

// Dispatcher handles store commands via async worker pool.
type Dispatcher struct {
	workers int
	jobs    chan job
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	stopped atomic.Bool
}

type job struct {
	ctx  context.Context
	cmd  dispatcher.Command
	emit dispatcher.Emitter
}

// NewDispatcher creates a store dispatcher with the specified worker count.
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
	d.stopped.Store(true)
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
	if d.stopped.Load() {
		return
	}
	select {
	case d.jobs <- job{ctx: ctx, cmd: cmd, emit: emit}:
	case <-d.ctx.Done():
	}
}

func (d *Dispatcher) execute(j job) {
	switch c := j.cmd.(type) {
	case *storeapi.StoreGetCmd:
		value, err := c.Store.Get(j.ctx, c.Key)
		j.emit.Emit(storeapi.StoreGetResponse{Value: value, Error: err}, nil)

	case *storeapi.StoreSetCmd:
		err := c.Store.Set(j.ctx, c.Entry)
		j.emit.Emit(storeapi.StoreSetResponse{Error: err}, nil)

	case *storeapi.StoreDeleteCmd:
		err := c.Store.Delete(j.ctx, c.Key)
		j.emit.Emit(storeapi.StoreDeleteResponse{Error: err}, nil)

	case *storeapi.StoreHasCmd:
		exists, err := c.Store.Has(j.ctx, c.Key)
		j.emit.Emit(storeapi.StoreHasResponse{Exists: exists, Error: err}, nil)
	}
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	d.submit(ctx, cmd, emit)
	return nil
}

// RegisterAll registers all store handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	h := dispatcher.HandlerFunc(d.handle)
	register(storeapi.CmdStoreGet, h)
	register(storeapi.CmdStoreSet, h)
	register(storeapi.CmdStoreDelete, h)
	register(storeapi.CmdStoreHas, h)
}
