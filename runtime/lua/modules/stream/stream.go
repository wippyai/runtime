// Package stream provides utilities for handling streaming data with context awareness
package stream

import (
	"context"
	"fmt"
	"io"
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
	mu        sync.Mutex
	ctx       context.Context
}

// NewStream creates a new Stream with the provided context, reader and configuration.
// Returns an error if reader is nil or if configuration is invalid.
func NewStream(ctx context.Context, reader io.ReadCloser, cfg *Options) (*Stream, error) {
	if reader == nil {
		return nil, fmt.Errorf("%w: nil reader", ErrInvalidConfig)
	}
	if cfg == nil {
		cfg = NewStreamConfig(0)
	}

	return &Stream{
		reader: reader,
		config: cfg,
		ctx:    ctx,
	}, nil
}

// ReadChunk reads the next chunk of data based on the configured buffer size.
// Returns EOF when the stream is exhausted.
func (s *Stream) ReadChunk() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	buffer := make([]byte, s.config.bufferSize)
	data, err := s.readDirect(buffer)
	if err != nil {
		if err == io.EOF {
			return nil, err
		}
		return nil, fmt.Errorf("read error: %w", err)
	}

	n := len(data)
	s.bytesRead += int64(n)
	return data, nil
}

func (s *Stream) readDirect(buffer []byte) ([]byte, error) {
	// Create a channel to receive read results
	resultCh := make(chan struct {
		n   int
		err error
	}, 1)

	// Start read operation in a goroutine
	go func() {
		n, err := s.reader.Read(buffer)
		select {
		case resultCh <- struct {
			n   int
			err error
		}{n, err}:
		case <-s.ctx.Done():
			// Read completed but context was cancelled before we could send
		}
	}()

	// Wait for either context cancellation or read completion
	select {
	case <-s.ctx.Done():
		return nil, fmt.Errorf("read cancelled: %w", s.ctx.Err())
	case result := <-resultCh:
		if result.err != nil {
			return nil, fmt.Errorf("direct read error: %w", result.err)
		}
		return buffer[:result.n], nil
	}
}

// BytesRead returns the total number of bytes read from the stream so far.
// This operation is concurrent-safe.
func (s *Stream) BytesRead() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.bytesRead
}

// Close closes the underlying reader and releases associated resources.
// Returns an error if the close operation fails.
func (s *Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.reader.Close(); err != nil {
		return fmt.Errorf("stream close error: %w", err)
	}
	return nil
}
