// Package store provides store command handlers for the dispatcher system.
package store

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/store"
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
	ctx      context.Context
	cmd      dispatcher.Command
	tag      uint64
	receiver process.ResultReceiver
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

func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver process.ResultReceiver) {
	if d.stopped.Load() {
		return
	}
	select {
	case d.jobs <- job{ctx: ctx, cmd: cmd, tag: tag, receiver: receiver}:
	case <-d.ctx.Done():
	}
}

func (d *Dispatcher) execute(j job) {
	switch c := j.cmd.(type) {
	case *store.StoreGetCmd:
		value, err := c.Store.Get(j.ctx, c.Key)
		j.receiver.CompleteYield(j.tag, store.StoreGetResponse{Value: value, Error: err}, nil)

	case *store.StoreSetCmd:
		err := c.Store.Set(j.ctx, c.Entry)
		j.receiver.CompleteYield(j.tag, store.StoreSetResponse{Error: err}, nil)

	case *store.StoreDeleteCmd:
		err := c.Store.Delete(j.ctx, c.Key)
		notFound := errors.Is(err, store.ErrKeyNotFound)
		j.receiver.CompleteYield(j.tag, store.StoreDeleteResponse{NotFound: notFound, Error: err}, nil)

	case *store.StoreHasCmd:
		exists, err := c.Store.Has(j.ctx, c.Key)
		j.receiver.CompleteYield(j.tag, store.StoreHasResponse{Exists: exists, Error: err}, nil)

	default:
		// unknown command type, ignore
	}
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver process.ResultReceiver) error {
	d.submit(ctx, cmd, tag, receiver)
	return nil
}

// RegisterAll registers all store handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	h := dispatcher.HandlerFunc(d.handle)
	register(store.CmdStoreGet, h)
	register(store.CmdStoreSet, h)
	register(store.CmdStoreDelete, h)
	register(store.CmdStoreHas, h)
}
