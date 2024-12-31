package stream

import (
	"context"
	"fmt"
	"io"
	"time"
)

var (
	ErrMaxSizeExceeded = fmt.Errorf("stream max size exceeded")
	ErrReadTimeout     = fmt.Errorf("stream read timeout")
	ErrInvalidConfig   = fmt.Errorf("invalid Stream configuration")
)

// Config holds configuration for Stream operations
type Config struct {
	bufferSize int64
	timeout    time.Duration
	maxSize    int64
}

// NewStreamConfig creates a new configuration with validation
func NewStreamConfig(bufferSize, maxSize int64, timeout time.Duration) (*Config, error) {
	if bufferSize <= 0 {
		bufferSize = 32 * 1024 // Default 32KB buffer
	}
	if maxSize < 0 {
		return nil, fmt.Errorf("%w: negative max size", ErrInvalidConfig)
	}
	if timeout < 0 {
		return nil, fmt.Errorf("%w: negative timeout", ErrInvalidConfig)
	}

	return &Config{
		bufferSize: bufferSize,
		timeout:    timeout,
		maxSize:    maxSize,
	}, nil
}

// readResult holds the result of an asynchronous read operation
type readResult struct {
	data []byte
	err  error
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
		var err error
		cfg, err = NewStreamConfig(0, 0, 0)
		if err != nil {
			return nil, err
		}
	}

	return &Stream{
		reader: reader,
		config: cfg,
		ctx:    ctx,
	}, nil
}

// ReadChunk reads the next chunk of data
func (s *Stream) ReadChunk() ([]byte, error) {
	if err := s.checkMaxSize(); err != nil {
		return nil, err
	}

	buffer := make([]byte, s.config.bufferSize)

	var data []byte
	var err error

	if s.config.timeout > 0 {
		data, err = s.readWithTimeout(buffer)
	} else {
		data, err = s.readDirect(buffer)
	}

	if err != nil {
		if err == io.EOF {
			return nil, err
		}
		return nil, fmt.Errorf("read error: %w", err)
	}

	n := len(data)
	if s.config.maxSize > 0 && s.bytesRead+int64(n) > s.config.maxSize {
		return nil, ErrMaxSizeExceeded
	}

	s.bytesRead += int64(n)
	return data, nil
}

func (s *Stream) checkMaxSize() error {
	if s.config.maxSize > 0 && s.bytesRead >= s.config.maxSize {
		return ErrMaxSizeExceeded
	}
	return nil
}

func (s *Stream) readDirect(buffer []byte) ([]byte, error) {
	n, err := s.reader.Read(buffer)
	if err != nil {
		return nil, fmt.Errorf("direct read error: %w", err)
	}
	return buffer[:n], nil
}

func (s *Stream) readWithTimeout(buffer []byte) ([]byte, error) {
	resultChan := make(chan readResult, 1)

	go func() {
		n, err := s.reader.Read(buffer)
		if err != nil {
			resultChan <- readResult{nil, fmt.Errorf("async read error: %w", err)}
			return
		}
		resultChan <- readResult{buffer[:n], nil}
	}()

	select {
	case result := <-resultChan:
		if result.err != nil {
			return nil, result.err
		}
		return result.data, nil
	case <-time.After(s.config.timeout):
		return nil, ErrReadTimeout
	case <-s.ctx.Done():
		return nil, fmt.Errorf("stream context done: %w", s.ctx.Err())
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
