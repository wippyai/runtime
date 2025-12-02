// Package excel provides excel command handlers for the dispatcher system.
package excel

import (
	"context"
	"io"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	excelapi "github.com/wippyai/runtime/api/dispatcher/excel"
	streamhandler "github.com/wippyai/runtime/service/fs/stream"
	"github.com/xuri/excelize/v2"
)

// streamRegistryReader wraps StreamRegistry to implement io.Reader.
type streamRegistryReader struct {
	registry *streamhandler.StreamRegistry
	streamID uint64
	eof      bool
}

func (r *streamRegistryReader) Read(p []byte) (n int, err error) {
	if r.eof {
		return 0, io.EOF
	}

	data, err := r.registry.Read(r.streamID, int64(len(p)))
	if err == io.EOF {
		r.eof = true
		if len(data) > 0 {
			copy(p, data)
			return len(data), nil
		}
		return 0, io.EOF
	}
	if err != nil {
		return 0, err
	}

	copy(p, data)
	return len(data), nil
}

// streamRegistryWriter wraps StreamRegistry to implement io.Writer.
type streamRegistryWriter struct {
	registry *streamhandler.StreamRegistry
	streamID uint64
}

func (w *streamRegistryWriter) Write(p []byte) (n int, err error) {
	return w.registry.Write(w.streamID, p)
}

// Dispatcher handles excel commands via async worker pool.
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

// NewDispatcher creates an excel dispatcher with the specified worker count.
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
	registry := streamhandler.GetStreamRegistry(j.ctx)
	if registry == nil {
		switch j.cmd.(type) {
		case *excelapi.ExcelOpenStreamCmd:
			j.emit.Emit(excelapi.ExcelOpenStreamResponse{Error: streamhandler.ErrStreamNotFound}, nil)
		case *excelapi.ExcelWriteStreamCmd:
			j.emit.Emit(excelapi.ExcelWriteStreamResponse{Error: streamhandler.ErrStreamNotFound}, nil)
		}
		return
	}

	switch c := j.cmd.(type) {
	case *excelapi.ExcelOpenStreamCmd:
		reader := &streamRegistryReader{
			registry: registry,
			streamID: c.StreamID,
		}
		file, err := excelize.OpenReader(reader)
		if err != nil {
			j.emit.Emit(excelapi.ExcelOpenStreamResponse{Error: err}, nil)
			return
		}
		j.emit.Emit(excelapi.ExcelOpenStreamResponse{File: file}, nil)

	case *excelapi.ExcelWriteStreamCmd:
		writer := &streamRegistryWriter{
			registry: registry,
			streamID: c.StreamID,
		}
		err := c.File.Write(writer)
		j.emit.Emit(excelapi.ExcelWriteStreamResponse{Error: err}, nil)
	}
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	d.submit(ctx, cmd, emit)
	return nil
}

// RegisterAll registers all excel handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	h := dispatcher.HandlerFunc(d.handle)
	register(excelapi.CmdExcelOpenStream, h)
	register(excelapi.CmdExcelWriteStream, h)
}
