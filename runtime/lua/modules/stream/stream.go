package stream

import (
	"context"
	"fmt"
	"io"
)

var (
	ErrInvalidConfig = fmt.Errorf("invalid Stream configuration")
)

// Config holds configuration for Stream operations
type Config struct {
	bufferSize int64
}

// NewStreamConfig creates a new configuration
func NewStreamConfig(bufferSize int64) *Config {
	if bufferSize <= 0 {
		bufferSize = 32 * 1024 // Default 32KB buffer
	}
	return &Config{
		bufferSize: bufferSize,
	}
}

// Stream handles streaming data from a reader
type Stream struct {
	reader    io.ReadCloser
	config    *Config
	bytesRead int64
	ctx       context.Context
}

// NewStream creates a new Stream with configuration
func NewStream(ctx context.Context, reader io.ReadCloser, cfg *Config) (*Stream, error) {
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

// ReadChunk reads the next chunk of data
func (s *Stream) ReadChunk() ([]byte, error) {
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

func (s *Stream) BytesRead() int64 {
	return s.bytesRead
}

func (s *Stream) Close() error {
	if err := s.reader.Close(); err != nil {
		return fmt.Errorf("stream close error: %w", err)
	}
	return nil
}
