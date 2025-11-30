// Package stream provides stream command handlers for the dispatcher system.
// Supports both blocking (for testing) and async (for production) execution modes.
package stream

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	streamapi "github.com/wippyai/runtime/api/dispatcher/stream"
	"github.com/wippyai/runtime/api/resource"
)

const DefaultChunkSize = 32 * 1024 // 32KB

// TypeStream is the type ID for stream entries in the resource table.
const TypeStream uint32 = 0x10

// ErrStreamNotFound is returned when stream ID doesn't exist.
var ErrStreamNotFound = errors.New("stream not found")

// ErrStreamClosed is returned when stream was already closed.
var ErrStreamClosed = errors.New("stream closed")

// ErrNotReadable is returned when trying to read from a write-only stream.
var ErrNotReadable = errors.New("stream is not readable")

// ErrNotWritable is returned when trying to write to a read-only stream.
var ErrNotWritable = errors.New("stream is not writable")

// ErrNotSeekable is returned when trying to seek in a non-seekable stream.
var ErrNotSeekable = errors.New("stream is not seekable")

// StreamCapabilities describes what a stream can do.
type StreamCapabilities struct {
	Readable bool
	Writable bool
	Seekable bool
}

// streamEntry holds an active stream with its capabilities.
type streamEntry struct {
	closer  io.Closer
	reader  io.Reader
	writer  io.Writer
	seeker  io.Seeker
	flusher Flusher
	stater  Stater
	caps    StreamCapabilities
	size    int64 // -1 if unknown
	closed  bool
}

// Drop implements resource.Dropper for automatic cleanup.
func (e *streamEntry) Drop() {
	if !e.closed && e.closer != nil {
		e.closed = true
		e.closer.Close()
	}
}

// Flusher is an optional interface for streams that support flush.
type Flusher interface {
	Flush() error
}

// Stater is an optional interface for streams that support stat.
type Stater interface {
	Stat() (size int64, err error)
}

// StreamRegistry manages active streams using the resource table.
type StreamRegistry struct {
	streams *resource.TypedTable[*streamEntry]
}

// NewStreamRegistry creates a stream registry backed by the given table.
func NewStreamRegistry(table *resource.Table) *StreamRegistry {
	return &StreamRegistry{
		streams: resource.NewTypedTable[*streamEntry](table, TypeStream),
	}
}

// Register adds a read-only stream to the registry (backward compatible).
func (r *StreamRegistry) Register(reader io.ReadCloser) uint64 {
	return r.RegisterStream(reader)
}

// RegisterStream adds any stream to the registry, detecting its capabilities.
func (r *StreamRegistry) RegisterStream(stream io.Closer) uint64 {
	entry := &streamEntry{
		closer: stream,
		size:   -1,
		closed: false,
	}

	// Detect capabilities
	if rd, ok := stream.(io.Reader); ok {
		entry.reader = rd
		entry.caps.Readable = true
	}
	if wr, ok := stream.(io.Writer); ok {
		entry.writer = wr
		entry.caps.Writable = true
	}
	if sk, ok := stream.(io.Seeker); ok {
		entry.seeker = sk
		entry.caps.Seekable = true
	}
	if fl, ok := stream.(Flusher); ok {
		entry.flusher = fl
	}
	if st, ok := stream.(Stater); ok {
		entry.stater = st
	}

	handle := r.streams.Insert(entry)
	return uint64(handle)
}

// RegisterWithSize adds a stream with known size.
func (r *StreamRegistry) RegisterWithSize(stream io.Closer, size int64) uint64 {
	id := r.RegisterStream(stream)
	if entry, ok := r.streams.Get(resource.Handle(id)); ok {
		entry.size = size
	}
	return id
}

// Capabilities returns the capabilities of a stream.
func (r *StreamRegistry) Capabilities(id uint64) (StreamCapabilities, error) {
	entry, ok := r.streams.Get(resource.Handle(id))
	if !ok {
		return StreamCapabilities{}, ErrStreamNotFound
	}
	if entry.closed {
		return StreamCapabilities{}, ErrStreamClosed
	}
	return entry.caps, nil
}

// Read reads a chunk from stream with given ID.
func (r *StreamRegistry) Read(id uint64, size int64) ([]byte, error) {
	entry, ok := r.streams.Get(resource.Handle(id))
	if !ok {
		return nil, ErrStreamNotFound
	}
	if entry.closed {
		return nil, ErrStreamClosed
	}
	if entry.reader == nil {
		return nil, ErrNotReadable
	}

	if size <= 0 {
		size = DefaultChunkSize
	}

	buf := make([]byte, size)
	n, err := entry.reader.Read(buf)

	if err == io.EOF {
		r.Close(id)
		if n > 0 {
			return buf[:n], nil
		}
		return nil, io.EOF
	}

	if err != nil {
		return nil, err
	}

	return buf[:n], nil
}

// Write writes data to stream with given ID.
func (r *StreamRegistry) Write(id uint64, data []byte) (int, error) {
	entry, ok := r.streams.Get(resource.Handle(id))
	if !ok {
		return 0, ErrStreamNotFound
	}
	if entry.closed {
		return 0, ErrStreamClosed
	}
	if entry.writer == nil {
		return 0, ErrNotWritable
	}

	return entry.writer.Write(data)
}

// Seek seeks to a position in the stream.
func (r *StreamRegistry) Seek(id uint64, offset int64, whence int) (int64, error) {
	entry, ok := r.streams.Get(resource.Handle(id))
	if !ok {
		return 0, ErrStreamNotFound
	}
	if entry.closed {
		return 0, ErrStreamClosed
	}
	if entry.seeker == nil {
		return 0, ErrNotSeekable
	}

	return entry.seeker.Seek(offset, whence)
}

// Flush flushes any buffered data to the underlying stream.
func (r *StreamRegistry) Flush(id uint64) error {
	entry, ok := r.streams.Get(resource.Handle(id))
	if !ok {
		return ErrStreamNotFound
	}
	if entry.closed {
		return ErrStreamClosed
	}
	if entry.flusher == nil {
		return nil
	}

	return entry.flusher.Flush()
}

// Stat returns information about the stream.
func (r *StreamRegistry) Stat(id uint64) (size int64, position int64, caps StreamCapabilities, err error) {
	entry, ok := r.streams.Get(resource.Handle(id))
	if !ok {
		return -1, -1, StreamCapabilities{}, ErrStreamNotFound
	}
	if entry.closed {
		return -1, -1, StreamCapabilities{}, ErrStreamClosed
	}

	size = entry.size
	position = int64(-1)

	if size < 0 && entry.stater != nil {
		size, _ = entry.stater.Stat()
	}

	if entry.seeker != nil {
		position, _ = entry.seeker.Seek(0, io.SeekCurrent)
	}

	return size, position, entry.caps, nil
}

// Close closes stream with given ID.
func (r *StreamRegistry) Close(id uint64) error {
	entry, ok := r.streams.Remove(resource.Handle(id))
	if !ok {
		return ErrStreamNotFound
	}
	if entry.closed {
		return nil
	}
	entry.closed = true
	if entry.closer != nil {
		return entry.closer.Close()
	}
	return nil
}

// TableProvider is implemented by types that provide a resource table.
type TableProvider interface {
	Table() *resource.Table
}

// GetStreamRegistry returns a StreamRegistry backed by the Table from context.
// Checks resource.StoreKey first (set by Lua engine), then falls back to legacy lookup.
// Returns nil if no Table is available in the context.
func GetStreamRegistry(ctx context.Context) *StreamRegistry {
	table := resource.GetTable(ctx)
	if table == nil {
		return nil
	}
	return NewStreamRegistry(table)
}

// GetOrCreateStreamRegistry returns a StreamRegistry for the context.
// Panics if no Store is available - engines must set resource.Store during initialization.
func GetOrCreateStreamRegistry(ctx context.Context) *StreamRegistry {
	registry := GetStreamRegistry(ctx)
	if registry == nil {
		panic("stream: no resource.Store in context - engine must set it during initialization")
	}
	return registry
}

// job represents a unit of work for the async dispatcher.
type job struct {
	ctx  context.Context
	cmd  dispatcher.Command
	emit dispatcher.EmitFunc
}

// Dispatcher handles stream commands with configurable execution mode.
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

// NewDispatcher creates a new stream dispatcher with the given configuration.
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

// execute runs the stream operation and emits the result.
func execute(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) {
	registry := GetStreamRegistry(ctx)
	if registry == nil {
		return
	}

	switch c := cmd.(type) {
	case streamapi.StreamReadCmd:
		data, err := registry.Read(c.StreamID, c.Size)
		if err == io.EOF {
			emit(nil)
			return
		}
		if err != nil {
			return
		}
		emit(data)

	case streamapi.StreamWriteCmd:
		n, err := registry.Write(c.StreamID, c.Data)
		if err != nil {
			return
		}
		emit(int64(n))

	case streamapi.StreamCloseCmd:
		if err := registry.Close(c.StreamID); err != nil {
			return
		}
		emit(nil)

	case streamapi.StreamSeekCmd:
		pos, err := registry.Seek(c.StreamID, c.Offset, c.Whence)
		if err != nil {
			return
		}
		emit(pos)

	case streamapi.StreamFlushCmd:
		if err := registry.Flush(c.StreamID); err != nil {
			return
		}
		emit(nil)

	case streamapi.StreamStatCmd:
		size, pos, caps, err := registry.Stat(c.StreamID)
		if err != nil {
			return
		}
		emit(streamapi.StreamInfo{
			Size:     size,
			Position: pos,
			Readable: caps.Readable,
			Writable: caps.Writable,
			Seekable: caps.Seekable,
		})
	}
}

// ReadHandler handles stream read commands.
type ReadHandler struct {
	d *Dispatcher
}

func (h *ReadHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// WriteHandler handles stream write commands.
type WriteHandler struct {
	d *Dispatcher
}

func (h *WriteHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// CloseHandler handles stream close commands.
type CloseHandler struct {
	d *Dispatcher
}

func (h *CloseHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// SeekHandler handles stream seek commands.
type SeekHandler struct {
	d *Dispatcher
}

func (h *SeekHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// FlushHandler handles stream flush commands.
type FlushHandler struct {
	d *Dispatcher
}

func (h *FlushHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// StatHandler handles stream stat commands.
type StatHandler struct {
	d *Dispatcher
}

func (h *StatHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// RegisterAll registers all stream handlers with the given registry function.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(streamapi.CmdStreamRead, &ReadHandler{d: d})
	register(streamapi.CmdStreamWrite, &WriteHandler{d: d})
	register(streamapi.CmdStreamClose, &CloseHandler{d: d})
	register(streamapi.CmdStreamSeek, &SeekHandler{d: d})
	register(streamapi.CmdStreamFlush, &FlushHandler{d: d})
	register(streamapi.CmdStreamStat, &StatHandler{d: d})
}

// Service is an alias for Dispatcher for backward compatibility.
type Service = Dispatcher

// NewService creates a blocking dispatcher for backward compatibility.
func NewService() *Dispatcher {
	return NewBlockingDispatcher()
}
