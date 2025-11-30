// Package excel provides excel command handlers for the dispatcher system.
// Supports both blocking (for testing) and async (for production) execution modes.
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

// job represents a unit of work for the async dispatcher.
type job struct {
	ctx  context.Context
	cmd  dispatcher.Command
	emit dispatcher.EmitFunc
}

// Dispatcher handles excel commands with configurable execution mode.
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

// NewDispatcher creates a new excel dispatcher with the given configuration.
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

// execute runs the excel operation and emits the result.
func execute(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) {
	registry := streamhandler.GetStreamRegistry(ctx)
	if registry == nil {
		emit(excelapi.ExcelOpenStreamResponse{Error: streamhandler.ErrStreamNotFound})
		return
	}

	switch c := cmd.(type) {
	case *excelapi.ExcelOpenStreamCmd:
		reader := &streamRegistryReader{
			registry: registry,
			streamID: c.StreamID,
		}
		file, err := excelize.OpenReader(reader)
		if err != nil {
			emit(excelapi.ExcelOpenStreamResponse{Error: err})
			return
		}
		emit(excelapi.ExcelOpenStreamResponse{File: file})

	case *excelapi.ExcelWriteStreamCmd:
		writer := &streamRegistryWriter{
			registry: registry,
			streamID: c.StreamID,
		}
		err := c.File.Write(writer)
		emit(excelapi.ExcelWriteStreamResponse{Error: err})
	}
}

// OpenStreamHandler handles excel open stream commands.
type OpenStreamHandler struct {
	d *Dispatcher
}

func (h *OpenStreamHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// WriteStreamHandler handles excel write stream commands.
type WriteStreamHandler struct {
	d *Dispatcher
}

func (h *WriteStreamHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// RegisterAll registers all excel handlers with the given registry function.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(excelapi.CmdExcelOpenStream, &OpenStreamHandler{d: d})
	register(excelapi.CmdExcelWriteStream, &WriteStreamHandler{d: d})
}

// Service is an alias for Dispatcher for backward compatibility.
type Service = Dispatcher

// NewService creates a blocking dispatcher for backward compatibility.
func NewService() *Dispatcher {
	return NewBlockingDispatcher()
}
