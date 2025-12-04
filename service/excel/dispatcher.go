// Package excel provides excel command handlers for the dispatcher system.
package excel

import (
	"context"
	"io"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	excelapi "github.com/wippyai/runtime/api/dispatcher/excel"
	"github.com/wippyai/runtime/api/runtime/resource"
	streamhandler "github.com/wippyai/runtime/service/fs/stream"
	"github.com/xuri/excelize/v2"
)

// streamReader wraps stream operations to implement io.Reader.
type streamReader struct {
	table    *resource.Table
	streamID uint64
	eof      bool
}

func (r *streamReader) Read(p []byte) (n int, err error) {
	if r.eof {
		return 0, io.EOF
	}

	data, err := streamhandler.Read(r.table, r.streamID, int64(len(p)))
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

// streamWriter wraps stream operations to implement io.Writer.
type streamWriter struct {
	table    *resource.Table
	streamID uint64
}

func (w *streamWriter) Write(p []byte) (n int, err error) {
	return streamhandler.Write(w.table, w.streamID, p)
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
	ctx      context.Context
	cmd      dispatcher.Command
	complete dispatcher.Completer
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

func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, complete dispatcher.Completer) {
	select {
	case d.jobs <- job{ctx: ctx, cmd: cmd, complete: complete}:
	case <-d.ctx.Done():
	}
}

func (d *Dispatcher) execute(j job) {
	table := resource.GetTable(j.ctx)
	if table == nil {
		switch j.cmd.(type) {
		case *excelapi.ExcelOpenStreamCmd:
			j.complete.Complete(excelapi.ExcelOpenStreamResponse{Error: streamhandler.ErrNoTable}, nil)
		case *excelapi.ExcelWriteStreamCmd:
			j.complete.Complete(excelapi.ExcelWriteStreamResponse{Error: streamhandler.ErrNoTable}, nil)
		}
		return
	}

	switch c := j.cmd.(type) {
	case *excelapi.ExcelOpenStreamCmd:
		reader := &streamReader{table: table, streamID: c.StreamID}
		file, err := excelize.OpenReader(reader)
		if err != nil {
			j.complete.Complete(excelapi.ExcelOpenStreamResponse{Error: err}, nil)
			return
		}
		j.complete.Complete(excelapi.ExcelOpenStreamResponse{File: file}, nil)

	case *excelapi.ExcelWriteStreamCmd:
		writer := &streamWriter{table: table, streamID: c.StreamID}
		err := c.File.Write(writer)
		j.complete.Complete(excelapi.ExcelWriteStreamResponse{Error: err}, nil)
	}
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, complete dispatcher.Completer) error {
	d.submit(ctx, cmd, complete)
	return nil
}

// RegisterAll registers all excel handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	h := dispatcher.HandlerFunc(d.handle)
	register(excelapi.CmdExcelOpenStream, h)
	register(excelapi.CmdExcelWriteStream, h)
}
