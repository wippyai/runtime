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

// Errors
var (
	ErrNotFound        = errors.New("stream not found")
	ErrClosed          = errors.New("stream closed")
	ErrNotReadable     = errors.New("stream is not readable")
	ErrNotWritable     = errors.New("stream is not writable")
	ErrNotSeekable     = errors.New("stream is not seekable")
	ErrNoTable         = errors.New("resource table not available")
	ErrScannerNotFound = errors.New("scanner not found")
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

	if errors.Is(err, io.EOF) {
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

// CreateScanner creates a scanner from a stream.
func CreateScanner(table *resource.Table, streamID uint64, splitType int) (uint64, error) {
	entry, err := Get(table, streamID)
	if err != nil {
		return 0, err
	}
	if entry.reader == nil {
		return 0, ErrNotReadable
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
		return nil, ErrScannerNotFound
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

func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) {
	select {
	case d.jobs <- job{ctx: ctx, cmd: cmd, tag: tag, receiver: receiver}:
	case <-d.ctx.Done():
	}
}

func (d *Dispatcher) execute(j job) {
	table := resource.GetTable(j.ctx)
	if table == nil {
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] execute error: no table\n")
		}
		j.receiver.CompleteYield(j.tag, nil, ErrNoTable)
		return
	}

	switch c := j.cmd.(type) {
	case streamapi.ReadCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] read id=%d size=%d\n", c.StreamID, c.Size)
		}
		data, err := Read(table, c.StreamID, c.Size)
		if errors.Is(err, io.EOF) {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] read id=%d EOF\n", c.StreamID)
			}
			j.receiver.CompleteYield(j.tag, nil, nil)
			return
		}
		if err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] read id=%d error=%v\n", c.StreamID, err)
			}
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] read id=%d bytes=%d\n", c.StreamID, len(data))
		}
		j.receiver.CompleteYield(j.tag, data, nil)

	case streamapi.WriteCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] write id=%d len=%d\n", c.StreamID, len(c.Data))
		}
		n, err := Write(table, c.StreamID, c.Data)
		if err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] write id=%d error=%v\n", c.StreamID, err)
			}
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] write id=%d written=%d\n", c.StreamID, n)
		}
		j.receiver.CompleteYield(j.tag, int64(n), nil)

	case streamapi.CloseCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] close id=%d\n", c.StreamID)
		}
		if err := Close(table, c.StreamID); err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] close id=%d error=%v\n", c.StreamID, err)
			}
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, nil, nil)

	case streamapi.SeekCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] seek id=%d offset=%d whence=%d\n", c.StreamID, c.Offset, c.Whence)
		}
		pos, err := Seek(table, c.StreamID, c.Offset, c.Whence)
		if err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] seek id=%d error=%v\n", c.StreamID, err)
			}
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] seek id=%d pos=%d\n", c.StreamID, pos)
		}
		j.receiver.CompleteYield(j.tag, pos, nil)

	case streamapi.FlushCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] flush id=%d\n", c.StreamID)
		}
		if err := Flush(table, c.StreamID); err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] flush id=%d error=%v\n", c.StreamID, err)
			}
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, nil, nil)

	case streamapi.StatCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] stat id=%d\n", c.StreamID)
		}
		size, pos, caps, err := Stat(table, c.StreamID)
		if err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] stat id=%d error=%v\n", c.StreamID, err)
			}
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] stat id=%d size=%d pos=%d readable=%v writable=%v seekable=%v\n",
				c.StreamID, size, pos, caps.Readable, caps.Writable, caps.Seekable)
		}
		j.receiver.CompleteYield(j.tag, streamapi.Info{
			Size:     size,
			Position: pos,
			Readable: caps.Readable,
			Writable: caps.Writable,
			Seekable: caps.Seekable,
		}, nil)

	case streamapi.ScannerCreateCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] scanner_create stream_id=%d split=%d\n", c.StreamID, c.SplitType)
		}
		scannerID, err := CreateScanner(table, c.StreamID, c.SplitType)
		if err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] scanner_create error=%v\n", err)
			}
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] scanner_create id=%d\n", scannerID)
		}
		j.receiver.CompleteYield(j.tag, scannerID, nil)

	case streamapi.ScannerScanCmd:
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] scanner_scan id=%d\n", c.ScannerID)
		}
		result, err := ScanNext(table, c.ScannerID)
		if err != nil {
			if d.debug != nil {
				fmt.Fprintf(d.debug, "[stream] scanner_scan id=%d error=%v\n", c.ScannerID, err)
			}
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		if d.debug != nil {
			fmt.Fprintf(d.debug, "[stream] scanner_scan id=%d has_token=%v\n", c.ScannerID, result.HasToken)
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
	register(streamapi.CmdRead, h)
	register(streamapi.CmdWrite, h)
	register(streamapi.CmdClose, h)
	register(streamapi.CmdSeek, h)
	register(streamapi.CmdFlush, h)
	register(streamapi.CmdStat, h)
	register(streamapi.CmdScannerCreate, h)
	register(streamapi.CmdScannerScan, h)
}
