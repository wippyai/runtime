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

	return &Stream{
		reader: reader,
		config: cfg,
		ctx:    ctx,
	}, nil
}

// ReadChunk reads the next chunk of data based on the configured buffer size.
// Returns EOF when the stream is exhausted.
func (s *Stream) ReadChunk() ([]byte, error) {
	s.rwmu.Lock()
	defer s.rwmu.Unlock()

	// TODO: sync pool
	buffer := make([]byte, s.config.bufferSize)
	n, err := s.readDirect(buffer)
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
	copy(resp, buffer)

	return resp, nil
}

func (s *Stream) readDirect(buffer []byte) (int, error) {
	// Spawn a channel to receive read results
	// todo: sync.Pool
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
			// todo: log
			// Read completed but context was canceled before we could send
		}
	}()

	// wait for either context cancellation or read completion
	select {
	case <-s.ctx.Done():
		return 0, fmt.Errorf("read canceled: %w", s.ctx.Err())
	case result := <-resultCh:
		if result.err != nil {
			return 0, fmt.Errorf("direct read error: %w", result.err)
		}

		return result.n, nil
	}
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

	if err := s.reader.Close(); err != nil {
		return fmt.Errorf("stream close error: %w", err)
	}
	return nil
}
