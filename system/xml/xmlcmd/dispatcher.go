// SPDX-License-Identifier: MPL-2.0

package xmlcmd

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/runtime/resource"
	xmlapi "github.com/wippyai/runtime/api/xml"
)

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

// Dispatcher executes XML stream commands on a worker pool.
type Dispatcher struct {
	ctx     context.Context
	jobs    chan job
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	workers int
}

type job struct {
	ctx      context.Context
	cmd      dispatcher.Command
	receiver dispatcher.ResultReceiver
	tag      uint64
}

// NewDispatcher creates an XML dispatcher with 16 workers by default.
func NewDispatcher(opts ...Option) *Dispatcher {
	d := &Dispatcher{workers: 16}
	for _, opt := range opts {
		opt(d)
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
	if d.cancel != nil {
		d.cancel()
	}
	if d.jobs != nil {
		close(d.jobs)
	}
	d.wg.Wait()
	d.jobs = nil
	d.cancel = nil
	return nil
}

func (d *Dispatcher) worker() {
	defer d.wg.Done()
	for j := range d.jobs {
		d.execute(j)
	}
}

func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) {
	j := job{ctx: ctx, cmd: cmd, tag: tag, receiver: receiver}
	if d.jobs == nil {
		d.execute(j)
		return
	}
	select {
	case d.jobs <- j:
	case <-d.ctx.Done():
	default:
		d.execute(j)
	}
}

func (d *Dispatcher) execute(j job) {
	j.receiver.CompleteYield(j.tag, nil, run(j.ctx, j.cmd))
}

// run executes one XML command against the resource table of ctx.
func run(ctx context.Context, cmd dispatcher.Command) error {
	table := resource.GetTable(ctx)
	if table == nil {
		return xmlapi.ErrNoTable
	}
	switch c := cmd.(type) {
	case xmlapi.FillCmd:
		r, err := GetReader(table, c.Reader)
		if err != nil {
			return err
		}
		return r.Fill(ctx)
	case xmlapi.NodeCmd:
		r, err := GetReader(table, c.Reader)
		if err != nil {
			return err
		}
		return r.Node(ctx)
	case xmlapi.SkipCmd:
		r, err := GetReader(table, c.Reader)
		if err != nil {
			return err
		}
		return r.Skip(ctx)
	case xmlapi.CopyCmd:
		r, err := GetReader(table, c.Reader)
		if err != nil {
			return err
		}
		w, err := GetWriter(table, c.Writer)
		if err != nil {
			return err
		}
		return r.Copy(ctx, w)
	case xmlapi.RecordsReadCmd:
		r, err := GetReader(table, c.Reader)
		if err != nil {
			return err
		}
		_, err = r.ReadRecords(ctx, c.Count)
		return err
	case xmlapi.FlushCmd:
		w, err := GetWriter(table, c.Writer)
		if err != nil {
			return err
		}
		return w.Flush(ctx, c.Sync)
	default:
		return apierror.WithDetails(apierror.Internal, fmt.Sprintf("unknown xml command: %T", cmd), attrs.Bag{"code": "argument"})
	}
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	d.submit(ctx, cmd, tag, receiver)
	return nil
}

// RegisterAll registers all XML handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	h := dispatcher.HandlerFunc(d.handle)
	register(xmlapi.ReaderFill, h)
	register(xmlapi.ReaderNode, h)
	register(xmlapi.ReaderSkip, h)
	register(xmlapi.ReaderCopy, h)
	register(xmlapi.RecordsRead, h)
	register(xmlapi.WriterFlush, h)
}
