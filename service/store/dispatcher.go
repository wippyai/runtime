// SPDX-License-Identifier: MPL-2.0

// Package store provides store command handlers for the dispatcher system.
package store

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/store"
)

// Dispatcher handles store commands via async worker pool.
type Dispatcher struct {
	ctx     context.Context
	jobs    chan job
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	workers int
	stopped atomic.Bool
}

type job struct {
	ctx      context.Context
	cmd      dispatcher.Command
	receiver dispatcher.ResultReceiver
	tag      uint64
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

func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) {
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
	case *store.GetCmd:
		value, err := c.Store.Get(j.ctx, c.Key)
		j.receiver.CompleteYield(j.tag, store.GetResponse{Value: value, Error: err}, nil)

	case *store.SetCmd:
		err := c.Store.Set(j.ctx, c.Entry)
		j.receiver.CompleteYield(j.tag, store.SetResponse{Error: err}, nil)

	case *store.DeleteCmd:
		err := c.Store.Delete(j.ctx, c.Key)
		notFound := errors.Is(err, store.ErrKeyNotFound)
		j.receiver.CompleteYield(j.tag, store.DeleteResponse{NotFound: notFound, Error: err}, nil)

	case *store.HasCmd:
		exists, err := c.Store.Has(j.ctx, c.Key)
		j.receiver.CompleteYield(j.tag, store.HasResponse{Exists: exists, Error: err}, nil)

	case *store.EntryCmd:
		entry, err := store.ReadEntry(j.ctx, c.Store, c.Key)
		j.receiver.CompleteYield(j.tag, store.EntryResponse{Entry: entry, Error: err}, nil)

	case *store.ListCmd:
		page, err := store.ListEntries(j.ctx, c.Store, c.Opts)
		j.receiver.CompleteYield(j.tag, store.ListResponse{Page: page, Error: err}, nil)

	case *store.PutCmd:
		entry, err := store.PutEntry(j.ctx, c.Store, c.Key, c.Value, c.Opts)
		j.receiver.CompleteYield(j.tag, store.PutResponse{Entry: entry, Error: err}, nil)

	default:
		// unknown command type, ignore
	}
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	d.submit(ctx, cmd, tag, receiver)
	return nil
}

// RegisterAll registers all store handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	h := dispatcher.HandlerFunc(d.handle)
	register(store.Get, h)
	register(store.Set, h)
	register(store.Delete, h)
	register(store.Has, h)
	register(store.EntryCommand, h)
	register(store.ListCommand, h)
	register(store.PutCommand, h)
}
