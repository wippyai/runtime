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
	// ErrInvalidReader indicates that a nil reader was provided
	ErrInvalidReader = fmt.Errorf("invalid reader")

	// DefaultChunkSize is the default size to read in chunks if not specified
	DefaultChunkSize int64 = 32 * 1024 // 32KB by default
)

// Stream handles streaming data from a reader with context awareness and
// concurrent-safe operations. It implements io.ReadCloser interface.
type Stream struct {
	reader    io.ReadCloser
	bytesRead int64
	rwmu      sync.RWMutex
	ctx       context.Context
}

// NewStream creates a new Stream with the provided context and reader.
// Returns an error if the reader is nil.
func NewStream(ctx context.Context, reader io.ReadCloser) (*Stream, error) {
	if reader == nil {
		return nil, ErrInvalidReader
	}

	stream := &Stream{
		reader: reader,
		ctx:    ctx,
	}

	return stream, nil
}

// Read implements io.Reader interface. It reads up to len(p) bytes into p
// with context-awareness and tracking of bytes read.
func (s *Stream) Read(p []byte) (n int, err error) {
	s.rwmu.Lock()
	defer s.rwmu.Unlock()

	if s.reader == nil {
		return 0, fmt.Errorf("stream closed")
	}

	if s.ctx.Err() != nil {
		return 0, fmt.Errorf("context canceled: %w", s.ctx.Err())
	}

	n, err = s.reader.Read(p)
	if err != nil {
		// fs.ErrClosed is returned when the process is stopped (the file is already closed)
		// all these errors are not critical and happen when the process (for example) is stopped
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
			return n, err
		}
		return n, fmt.Errorf("read error: %w", err)
	}

	s.bytesRead += int64(n)
	return n, nil
}

// ReadChunk reads a chunk of data with the specified size.
// If size <= 0, it uses DefaultChunkSize.
func (s *Stream) ReadChunk(size int64) ([]byte, error) {
	if size <= 0 {
		size = DefaultChunkSize
	}

	buffer := make([]byte, size)
	n, err := s.Read(buffer)
	if err != nil {
		return nil, err
	}

	// Return only the portion of the buffer that was filled
	chunk := make([]byte, n)
	copy(chunk, buffer[:n])
	return chunk, nil
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
