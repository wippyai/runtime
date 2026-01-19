// Package stream provides stream command handlers for the dispatcher system.
package stream

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/runtime/resource"
	streamapi "github.com/wippyai/runtime/api/stream"
)

const DefaultChunkSize = 32 * 1024 // 32KB

// TypeStream is the type ID for stream entries in the resource table.
const TypeStream uint32 = 0x10

// TypeScanner is the type ID for scanner entries in the resource table.
const TypeScanner uint32 = 0x11

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
		_ = e.closer.Close()
	}
}

// Caps returns the stream capabilities.
func (e *Entry) Caps() Capabilities {
	return e.caps
}

// Reader returns the underlying io.Reader, or nil if not readable.
func (e *Entry) Reader() io.Reader {
	return e.reader
}

// Writer returns the underlying io.Writer, or nil if not writable.
func (e *Entry) Writer() io.Writer {
	return e.writer
}

// ScannerEntry holds an active scanner.
type ScannerEntry struct {
	scanner  *bufio.Scanner
	lastText string
	lastErr  error
	done     bool
}

// Drop implements resource.Dropper for automatic cleanup.
func (s *ScannerEntry) Drop() {
	// Scanner doesn't need explicit cleanup, stream handles it
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
		return nil, streamapi.ErrNotFound
	}
	entry := val.(*Entry)
	if entry.closed {
		return nil, streamapi.ErrClosed
	}
	return entry, nil
}

// Read reads a chunk from stream with given ID.
// This allocates a new buffer. For high-performance code, use ReadBuffered.
func Read(table *resource.Table, id uint64, size int64) ([]byte, error) {
	buf, err := ReadBuffered(table, id, size)
	if err != nil {
		return nil, err
	}
	if buf == nil {
		return nil, nil
	}
	defer buf.Release()

	result := make([]byte, buf.N)
	copy(result, buf.Data[:buf.N])
	return result, nil
}

// ReadBuffered reads a chunk from stream using a pooled buffer.
// The caller MUST call buf.Release() when done with the data.
// Returns nil buffer on EOF with no data.
func ReadBuffered(table *resource.Table, id uint64, size int64) (*streamapi.Buffer, error) {
	entry, err := Get(table, id)
	if err != nil {
		return nil, err
	}
	if entry.reader == nil {
		return nil, streamapi.ErrNotReadable
	}

	if size <= 0 {
		size = DefaultChunkSize
	}

	buf := streamapi.AcquireBuffer(int(size))
	n, err := entry.reader.Read(buf.Data[:size])
	buf.N = n

	if errors.Is(err, io.EOF) {
		if n > 0 {
			return buf, nil
		}
		buf.Release()
		return nil, io.EOF
	}

	if err != nil {
		buf.Release()
		return nil, err
	}

	return buf, nil
}

// Write writes data to stream with given ID.
func Write(table *resource.Table, id uint64, data []byte) (int, error) {
	entry, err := Get(table, id)
	if err != nil {
		return 0, err
	}
	if entry.writer == nil {
		return 0, streamapi.ErrNotWritable
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
		return 0, streamapi.ErrNotSeekable
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
		return streamapi.ErrNotFound
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

// CreateScanner creates a scanner from a stream.
func CreateScanner(table *resource.Table, streamID uint64, splitType int) (uint64, error) {
	entry, err := Get(table, streamID)
	if err != nil {
		return 0, err
	}
	if entry.reader == nil {
		return 0, streamapi.ErrNotReadable
	}

	scanner := bufio.NewScanner(entry.reader)

	switch splitType {
	case streamapi.SplitLines:
		scanner.Split(bufio.ScanLines)
	case streamapi.SplitWords:
		scanner.Split(bufio.ScanWords)
	case streamapi.SplitBytes:
		scanner.Split(bufio.ScanBytes)
	case streamapi.SplitRunes:
		scanner.Split(bufio.ScanRunes)
	default:
		scanner.Split(bufio.ScanLines)
	}

	scanEntry := &ScannerEntry{
		scanner: scanner,
	}

	return uint64(table.Insert(TypeScanner, scanEntry)), nil
}

// GetScanner retrieves a scanner entry by handle.
func GetScanner(table *resource.Table, id uint64) (*ScannerEntry, error) {
	val, ok := table.GetTyped(resource.Handle(id), TypeScanner)
	if !ok {
		return nil, streamapi.ErrScannerNotFound
	}
	return val.(*ScannerEntry), nil
}

// ScanNext advances the scanner and returns the result.
func ScanNext(table *resource.Table, scannerID uint64) (streamapi.ScanResult, error) {
	entry, err := GetScanner(table, scannerID)
	if err != nil {
		return streamapi.ScanResult{}, err
	}

	if entry.done {
		return streamapi.ScanResult{
			HasToken: false,
			Text:     "",
			Error:    errString(entry.lastErr),
		}, nil
	}

	if entry.scanner.Scan() {
		entry.lastText = entry.scanner.Text()
		return streamapi.ScanResult{
			HasToken: true,
			Text:     entry.lastText,
			Error:    "",
		}, nil
	}

	entry.done = true
	entry.lastErr = entry.scanner.Err()
	return streamapi.ScanResult{
		HasToken: false,
		Text:     "",
		Error:    errString(entry.lastErr),
	}, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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

// Dispatcher handles stream commands via async worker pool.
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
	tag      uint64
	receiver dispatcher.ResultReceiver
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
	table := resource.GetTable(j.ctx)
	if table == nil {
		j.receiver.CompleteYield(j.tag, nil, streamapi.ErrNoTable)
		return
	}

	switch c := j.cmd.(type) {
	case streamapi.ReadCmd:
		buf, err := ReadBuffered(table, c.StreamID, c.Size)
		if errors.Is(err, io.EOF) {
			j.receiver.CompleteYield(j.tag, nil, nil)
			return
		}
		if err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, buf, nil)

	case streamapi.WriteCmd:
		n, err := Write(table, c.StreamID, c.Data)
		if err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, int64(n), nil)

	case streamapi.CloseCmd:
		if err := Close(table, c.StreamID); err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, nil, nil)

	case streamapi.SeekCmd:
		pos, err := Seek(table, c.StreamID, c.Offset, c.Whence)
		if err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, pos, nil)

	case streamapi.FlushCmd:
		if err := Flush(table, c.StreamID); err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, nil, nil)

	case streamapi.StatCmd:
		size, pos, caps, err := Stat(table, c.StreamID)
		if err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, streamapi.Info{
			Size:     size,
			Position: pos,
			Readable: caps.Readable,
			Writable: caps.Writable,
			Seekable: caps.Seekable,
		}, nil)

	case streamapi.ScannerCreateCmd:
		scannerID, err := CreateScanner(table, c.StreamID, c.SplitType)
		if err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, scannerID, nil)

	case streamapi.ScannerScanCmd:
		result, err := ScanNext(table, c.ScannerID)
		if err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, result, nil)

	default:
		j.receiver.CompleteYield(j.tag, nil, fmt.Errorf("unknown stream command: %T", j.cmd))
	}
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	d.submit(ctx, cmd, tag, receiver)
	return nil
}

// RegisterAll registers all stream handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	h := dispatcher.HandlerFunc(d.handle)
	register(streamapi.Read, h)
	register(streamapi.Write, h)
	register(streamapi.Close, h)
	register(streamapi.Seek, h)
	register(streamapi.Flush, h)
	register(streamapi.Stat, h)
	register(streamapi.ScannerCreate, h)
	register(streamapi.ScannerScan, h)
}
