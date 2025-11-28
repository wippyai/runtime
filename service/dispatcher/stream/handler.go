// Package stream provides stream-related command handlers for the dispatcher system.
package stream

import (
	"context"
	"errors"
	"io"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	streamapi "github.com/wippyai/runtime/api/dispatcher/stream"
)

const DefaultChunkSize = 32 * 1024 // 32KB

// StreamRegistryKey is the context key for StreamRegistry.
var StreamRegistryKey = &ctxapi.Key{Name: "stream.registry", Inherit: false}

// ErrStreamNotFound is returned when stream ID doesn't exist.
var ErrStreamNotFound = errors.New("stream not found")

// ErrStreamClosed is returned when stream was already closed.
var ErrStreamClosed = errors.New("stream closed")

// streamEntry holds an active stream.
type streamEntry struct {
	reader io.ReadCloser
	closed bool
}

// StreamRegistry manages active streams for a process.
// Thread-safe, stores streams by uint64 ID.
type StreamRegistry struct {
	mu      sync.Mutex
	streams map[uint64]*streamEntry
	nextID  uint64
}

// NewStreamRegistry creates a new stream registry.
func NewStreamRegistry() *StreamRegistry {
	return &StreamRegistry{
		streams: make(map[uint64]*streamEntry),
	}
}

// Register adds a stream to the registry and returns its ID.
func (r *StreamRegistry) Register(reader io.ReadCloser) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID++
	id := r.nextID

	r.streams[id] = &streamEntry{
		reader: reader,
		closed: false,
	}

	return id
}

// Read reads a chunk from stream with given ID.
// Returns bytes or error if stream not found or closed.
func (r *StreamRegistry) Read(id uint64, size int64) ([]byte, error) {
	r.mu.Lock()
	entry, ok := r.streams[id]
	r.mu.Unlock()

	if !ok {
		return nil, ErrStreamNotFound
	}

	if entry.closed {
		return nil, ErrStreamClosed
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

// Close closes stream with given ID.
func (r *StreamRegistry) Close(id uint64) error {
	r.mu.Lock()
	entry, ok := r.streams[id]
	if ok {
		delete(r.streams, id)
	}
	r.mu.Unlock()

	if !ok {
		return ErrStreamNotFound
	}

	if entry.closed {
		return nil
	}

	entry.closed = true
	return entry.reader.Close()
}

// CloseAll closes all streams and clears the registry.
func (r *StreamRegistry) CloseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, entry := range r.streams {
		if !entry.closed {
			entry.closed = true
			entry.reader.Close()
		}
		delete(r.streams, id)
	}
}

// GetStreamRegistry retrieves StreamRegistry from FrameContext.
func GetStreamRegistry(ctx context.Context) *StreamRegistry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(StreamRegistryKey); ok {
		return val.(*StreamRegistry)
	}
	return nil
}

// SetStreamRegistry stores StreamRegistry in FrameContext.
func SetStreamRegistry(ctx context.Context, r *StreamRegistry) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(StreamRegistryKey, r)
}

// GetOrCreateStreamRegistry returns existing registry or creates a new one.
func GetOrCreateStreamRegistry(ctx context.Context) *StreamRegistry {
	if r := GetStreamRegistry(ctx); r != nil {
		return r
	}
	r := NewStreamRegistry()
	SetStreamRegistry(ctx, r)
	return r
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

// Service bundles all stream handlers for convenient registration.
type Service struct {
	Read  *StreamReadHandler
	Close *StreamCloseHandler
}

// NewService creates a new stream service with all handlers initialized.
func NewService() *Service {
	return &Service{
		Read:  NewStreamReadHandler(),
		Close: NewStreamCloseHandler(),
	}
}

// RegisterAll registers all stream handlers with the given registry function.
func (s *Service) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(streamapi.CmdStreamRead, s.Read)
	register(streamapi.CmdStreamClose, s.Close)
}
