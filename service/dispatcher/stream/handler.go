// Package stream provides stream-related command handlers for the dispatcher system.
package stream

import (
	"context"
	"errors"
	"io"

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

// StreamReadHandler reads a chunk from a stream.
type StreamReadHandler struct{}

func NewStreamReadHandler() *StreamReadHandler {
	return &StreamReadHandler{}
}

func (h *StreamReadHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	readCmd := cmd.(streamapi.StreamReadCmd)

	registry := GetStreamRegistry(ctx)
	if registry == nil {
		return ErrStreamNotFound
	}

	data, err := registry.Read(readCmd.StreamID, readCmd.Size)
	if err == io.EOF {
		emit(nil) // nil signals EOF
		return nil
	}
	if err != nil {
		return err
	}

	emit(data)
	return nil
}

// StreamCloseHandler closes a stream.
type StreamCloseHandler struct{}

func NewStreamCloseHandler() *StreamCloseHandler {
	return &StreamCloseHandler{}
}

func (h *StreamCloseHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	closeCmd := cmd.(streamapi.StreamCloseCmd)

	registry := GetStreamRegistry(ctx)
	if registry == nil {
		return ErrStreamNotFound
	}

	return registry.Close(closeCmd.StreamID)
}

// StreamWriteHandler writes data to a stream.
type StreamWriteHandler struct{}

func NewStreamWriteHandler() *StreamWriteHandler {
	return &StreamWriteHandler{}
}

func (h *StreamWriteHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	writeCmd := cmd.(streamapi.StreamWriteCmd)

	registry := GetStreamRegistry(ctx)
	if registry == nil {
		return ErrStreamNotFound
	}

	n, err := registry.Write(writeCmd.StreamID, writeCmd.Data)
	if err != nil {
		return err
	}

	emit(int64(n))
	return nil
}

// StreamSeekHandler seeks within a stream.
type StreamSeekHandler struct{}

func NewStreamSeekHandler() *StreamSeekHandler {
	return &StreamSeekHandler{}
}

func (h *StreamSeekHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	seekCmd := cmd.(streamapi.StreamSeekCmd)

	registry := GetStreamRegistry(ctx)
	if registry == nil {
		return ErrStreamNotFound
	}

	pos, err := registry.Seek(seekCmd.StreamID, seekCmd.Offset, seekCmd.Whence)
	if err != nil {
		return err
	}

	emit(pos)
	return nil
}

// StreamFlushHandler flushes buffered data.
type StreamFlushHandler struct{}

func NewStreamFlushHandler() *StreamFlushHandler {
	return &StreamFlushHandler{}
}

func (h *StreamFlushHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	flushCmd := cmd.(streamapi.StreamFlushCmd)

	registry := GetStreamRegistry(ctx)
	if registry == nil {
		return ErrStreamNotFound
	}

	return registry.Flush(flushCmd.StreamID)
}

// StreamStatHandler returns stream information.
type StreamStatHandler struct{}

func NewStreamStatHandler() *StreamStatHandler {
	return &StreamStatHandler{}
}

func (h *StreamStatHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	statCmd := cmd.(streamapi.StreamStatCmd)

	registry := GetStreamRegistry(ctx)
	if registry == nil {
		return ErrStreamNotFound
	}

	size, pos, caps, err := registry.Stat(statCmd.StreamID)
	if err != nil {
		return err
	}

	emit(streamapi.StreamInfo{
		Size:     size,
		Position: pos,
		Readable: caps.Readable,
		Writable: caps.Writable,
		Seekable: caps.Seekable,
	})
	return nil
}

// Service bundles all stream handlers for convenient registration.
type Service struct {
	Read  *StreamReadHandler
	Close *StreamCloseHandler
	Write *StreamWriteHandler
	Seek  *StreamSeekHandler
	Flush *StreamFlushHandler
	Stat  *StreamStatHandler
}

// NewService creates a new stream service with all handlers initialized.
func NewService() *Service {
	return &Service{
		Read:  NewStreamReadHandler(),
		Close: NewStreamCloseHandler(),
		Write: NewStreamWriteHandler(),
		Seek:  NewStreamSeekHandler(),
		Flush: NewStreamFlushHandler(),
		Stat:  NewStreamStatHandler(),
	}
}

// RegisterAll registers all stream handlers with the given registry function.
func (s *Service) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(streamapi.CmdStreamRead, s.Read)
	register(streamapi.CmdStreamClose, s.Close)
	register(streamapi.CmdStreamWrite, s.Write)
	register(streamapi.CmdStreamSeek, s.Seek)
	register(streamapi.CmdStreamFlush, s.Flush)
	register(streamapi.CmdStreamStat, s.Stat)
}
