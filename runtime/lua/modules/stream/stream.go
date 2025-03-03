// Package stream provides utilities for handling streaming data with context awareness
package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"
)

var (
	// ErrInvalidConfig indicates that the Stream configuration is invalid,
	// such as when a nil reader is provided
	ErrInvalidConfig = fmt.Errorf("invalid Stream configuration")
)

// Options holds configuration for Stream operations
type Options struct {
	bufferSize int64
}

// NewStreamConfig creates a new configuration with the specified buffer size.
// If bufferSize is <= 0, it defaults to 32KB.
func NewStreamConfig(bufferSize int64) *Options {
	if bufferSize <= 0 {
		bufferSize = 32 * 1024 // Default 32KB buffer
	}
	return &Options{
		bufferSize: bufferSize,
	}
}

// Stream handles streaming data from a reader with context awareness and
// concurrent-safe operations. It tracks bytes read and provides chunked reading
// capabilities.
type Stream struct {
	reader    io.ReadCloser
	config    *Options
	bytesRead int64
	rwmu      sync.RWMutex
	buffer    []byte
	ctx       context.Context
}

// NewStream creates a new Stream with the provided context, reader and configuration.
// Returns an error if the reader is nil or if the configuration is invalid.
func NewStream(ctx context.Context, reader io.ReadCloser, cfg *Options) (*Stream, error) {
	if reader == nil {
		return nil, fmt.Errorf("%w: nil reader", ErrInvalidConfig)
	}
	if cfg == nil {
		cfg = NewStreamConfig(0)
	}

	stream := &Stream{
		reader: reader,
		config: cfg,
		ctx:    ctx,
		buffer: make([]byte, cfg.bufferSize),
	}

	/**
	Attention, you have to clean stream at parent level.
	*/

	return stream, nil
}

// ReadChunk reads the next chunk of data based on the configured buffer size.
// Returns EOF when the stream is exhausted.
func (s *Stream) ReadChunk() ([]byte, error) {
	s.rwmu.Lock()
	defer s.rwmu.Unlock()

	if s.reader == nil {
		return nil, fmt.Errorf("stream closed")
	}

	if s.ctx.Err() != nil {
		return nil, fmt.Errorf("context canceled: %w", s.ctx.Err())
	}

	n, err := s.reader.Read(s.buffer)
	if err != nil {
		// fs.ErrClosed is returned when the process is stopped (the file is already closed)
		// all these errors are not critical and happen when the process (for example) is stopped
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
			return nil, err
		}
		return nil, fmt.Errorf("read error: %w", err)
	}

	s.bytesRead += int64(n)
	resp := make([]byte, n)
	copy(resp, s.buffer)
	s.buffer = s.buffer[0:]

	return resp, nil
}

// BytesRead returns the total number of bytes read from the stream so far.
// This operation is concurrent-safe.
func (s *Stream) BytesRead() int64 {
	s.rwmu.RLock()
	defer s.rwmu.RUnlock()

	return s.bytesRead
}

// Close closes the underlying reader and releases associated resources.
// Returns an error if the close operation fails.
func (s *Stream) Close() error {
	s.rwmu.Lock()
	defer s.rwmu.Unlock()

	if s.reader != nil {
		if err := s.reader.Close(); err != nil {
			return fmt.Errorf("stream close error: %w", err)
		}
		s.reader = nil
	}

	return nil
}
