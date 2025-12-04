// Package stream provides stream command handlers for the dispatcher system.
package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	streamapi "github.com/wippyai/runtime/api/dispatcher/stream"
	"github.com/wippyai/runtime/api/runtime/resource"
)

const DefaultChunkSize = 32 * 1024 // 32KB

// TypeStream is the type ID for stream entries in the resource table.
const TypeStream uint32 = 0x10

// Errors
var (
	ErrNotFound    = errors.New("stream not found")
	ErrClosed      = errors.New("stream closed")
	ErrNotReadable = errors.New("stream is not readable")
	ErrNotWritable = errors.New("stream is not writable")
	ErrNotSeekable = errors.New("stream is not seekable")
	ErrNoTable     = errors.New("resource table not available")
)

// Capabilities describes what a stream can do.
type Capabilities struct {
	Readable bool
	Writable bool
	Seekable bool
}

// Entry holds an active stream with its capabilities.
type Entry struct {
	closer  io.Closer
	reader  io.Reader
	writer  io.Writer
	seeker  io.Seeker
	flusher Flusher
	stater  Stater
	caps    Capabilities
	size    int64
	closed  bool
}

// Drop implements resource.Dropper for automatic cleanup.
func (e *Entry) Drop() {
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

// Insert adds a stream to the table, detecting its capabilities.
// Returns the handle as uint64.
func Insert(table *resource.Table, stream io.Closer) uint64 {
	return InsertWithSize(table, stream, -1)
}

// InsertWithSize adds a stream with known size to the table.
func InsertWithSize(table *resource.Table, stream io.Closer, size int64) uint64 {
	entry := &Entry{
		closer: stream,
		size:   size,
		closed: false,
	}

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

	return uint64(table.Insert(TypeStream, entry))
}

// Get retrieves a stream entry by handle.
func Get(table *resource.Table, id uint64) (*Entry, error) {
	val, ok := table.GetTyped(resource.Handle(id), TypeStream)
	if !ok {
		if v, exists := table.Get(resource.Handle(id)); exists {
			return nil, fmt.Errorf("stream %d exists but wrong type: %T", id, v)
		}
		return nil, ErrNotFound
	}
	entry := val.(*Entry)
	if entry.closed {
		return nil, ErrClosed
	}
	return entry, nil
}

// Read reads a chunk from stream with given ID.
func Read(table *resource.Table, id uint64, size int64) ([]byte, error) {
	entry, err := Get(table, id)
	if err != nil {
		return nil, err
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
func Write(table *resource.Table, id uint64, data []byte) (int, error) {
	entry, err := Get(table, id)
	if err != nil {
		return 0, err
	}
	if entry.writer == nil {
		return 0, ErrNotWritable
	}

	return entry.writer.Write(data)
}

// Seek seeks to a position in the stream.
func Seek(table *resource.Table, id uint64, offset int64, whence int) (int64, error) {
	entry, err := Get(table, id)
	if err != nil {
		return 0, err
	}
	if entry.seeker == nil {
		return 0, ErrNotSeekable
	}

	return entry.seeker.Seek(offset, whence)
}

// Flush flushes any buffered data to the underlying stream.
func Flush(table *resource.Table, id uint64) error {
	entry, err := Get(table, id)
	if err != nil {
		return err
	}
	if entry.flusher == nil {
		return nil
	}

	return entry.flusher.Flush()
}

// Stat returns information about the stream.
func Stat(table *resource.Table, id uint64) (size int64, position int64, caps Capabilities, err error) {
	entry, err := Get(table, id)
	if err != nil {
		return -1, -1, Capabilities{}, err
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
func Close(table *resource.Table, id uint64) error {
	val, ok := table.Remove(resource.Handle(id))
	if !ok {
		return ErrNotFound
	}
	entry := val.(*Entry)
	if entry.closed {
		return nil
	}
	entry.closed = true
	if entry.closer != nil {
		return entry.closer.Close()
	}
	return nil
}

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

// WithDebug enables debug output to the given writer.
func WithDebug(w io.Writer) Option {
	return func(d *Dispatcher) {
		d.debug = w
	}
}

// Dispatcher handles stream commands via async worker pool.
type Dispatcher struct {
	workers int
	jobs    chan job
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	debug   io.Writer
}

type job struct {
	ctx      context.Context
	cmd      dispatcher.Command
	complete dispatcher.Completer
}

// NewDispatcher creates a stream dispatcher with default 16 workers.
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

	if d.debug != nil {
		fmt.Fprintf(d.debug, "[stream] dispatcher started workers=%d\n", d.workers)
	}
	return nil
}

// Stop shuts down the dispatcher and drains pending jobs.
func (d *Dispatcher) Stop(_ context.Context) error {
	if d.debug != nil {
		fmt.Fprintf(d.debug, "[stream] dispatcher stopping\n")
	}
	d.cancel()
	close(d.jobs)
	d.wg.Wait()
	if d.debug != nil {
		fmt.Fprintf(d.debug, "[stream] dispatcher stopped\n")
	}
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
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] execute error: no table\n")
		}
		j.complete.Complete(nil, ErrNoTable)
		return
	}

	switch c := j.cmd.(type) {
	case streamapi.ReadCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] read id=%d size=%d\n", c.StreamID, c.Size)
		}
		data, err := Read(table, c.StreamID, c.Size)
		if err == io.EOF {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] read id=%d EOF\n", c.StreamID)
			}
			j.complete.Complete(nil, nil)
			return
		}
		if err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] read id=%d error=%v\n", c.StreamID, err)
			}
			j.complete.Complete(nil, err)
			return
		}
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] read id=%d bytes=%d\n", c.StreamID, len(data))
		}
		j.complete.Complete(data, nil)

	case streamapi.WriteCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] write id=%d len=%d\n", c.StreamID, len(c.Data))
		}
		n, err := Write(table, c.StreamID, c.Data)
		if err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] write id=%d error=%v\n", c.StreamID, err)
			}
			j.complete.Complete(nil, err)
			return
		}
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] write id=%d written=%d\n", c.StreamID, n)
		}
		j.complete.Complete(int64(n), nil)

	case streamapi.CloseCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] close id=%d\n", c.StreamID)
		}
		if err := Close(table, c.StreamID); err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] close id=%d error=%v\n", c.StreamID, err)
			}
			j.complete.Complete(nil, err)
			return
		}
		j.complete.Complete(nil, nil)

	case streamapi.SeekCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] seek id=%d offset=%d whence=%d\n", c.StreamID, c.Offset, c.Whence)
		}
		pos, err := Seek(table, c.StreamID, c.Offset, c.Whence)
		if err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] seek id=%d error=%v\n", c.StreamID, err)
			}
			j.complete.Complete(nil, err)
			return
		}
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] seek id=%d pos=%d\n", c.StreamID, pos)
		}
		j.complete.Complete(pos, nil)

	case streamapi.FlushCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] flush id=%d\n", c.StreamID)
		}
		if err := Flush(table, c.StreamID); err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] flush id=%d error=%v\n", c.StreamID, err)
			}
			j.complete.Complete(nil, err)
			return
		}
		j.complete.Complete(nil, nil)

	case streamapi.StatCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] stat id=%d\n", c.StreamID)
		}
		size, pos, caps, err := Stat(table, c.StreamID)
		if err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] stat id=%d error=%v\n", c.StreamID, err)
			}
			j.complete.Complete(nil, err)
			return
		}
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] stat id=%d size=%d pos=%d readable=%v writable=%v seekable=%v\n",
				c.StreamID, size, pos, caps.Readable, caps.Writable, caps.Seekable)
		}
		j.complete.Complete(streamapi.Info{
			Size:     size,
			Position: pos,
			Readable: caps.Readable,
			Writable: caps.Writable,
			Seekable: caps.Seekable,
		}, nil)

	default:
		j.complete.Complete(nil, fmt.Errorf("unknown stream command: %T", j.cmd))
	}
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, complete dispatcher.Completer) error {
	d.submit(ctx, cmd, complete)
	return nil
}

// RegisterAll registers all stream handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	h := dispatcher.HandlerFunc(d.handle)
	register(streamapi.CmdRead, h)
	register(streamapi.CmdWrite, h)
	register(streamapi.CmdClose, h)
	register(streamapi.CmdSeek, h)
	register(streamapi.CmdFlush, h)
	register(streamapi.CmdStat, h)
}
