// stream.go
package stream

import (
	"context"
	"fmt"
	"github.com/ponyruntime/go-lua"
	"go.uber.org/zap"
	"io"
	"time"
)

var (
	ErrMaxSizeExceeded = fmt.Errorf("stream max size exceeded")
	ErrReadTimeout     = fmt.Errorf("stream read timeout")
	ErrInvalidConfig   = fmt.Errorf("invalid stream configuration")
)

// Module represents the stream Lua module
type Module struct {
	log *zap.Logger
}

// NewStreamModule creates a new stream module (internal)
func NewStreamModule(log *zap.Logger) *Module {
	return &Module{log: log}
}

// Name returns the module name
func (m *Module) Name() string {
	return "stream"
}

// Loader registers the module functions and constants
func (m *Module) Loader(l *lua.LState) int {
	// Create module table
	mod := l.NewTable()

	registerStream(l, mod)

	l.Push(mod)
	return 1
}

// registerStream registers the stream type in Lua
func registerStream(l *lua.LState, mod *lua.LTable) {
	// Create and register the stream metatable
	mt := l.NewTypeMetatable("stream")
	l.SetField(mt, "__index", mt)

	// Register methods
	l.SetFuncs(mt, map[string]lua.LGFunction{
		"read":       streamRead,
		"close":      streamClose,
		"bytes_read": streamBytesRead,
		"__call":     streamIter,
	})
}

// streamConfig holds configuration for stream operations
type streamConfig struct {
	bufferSize int64
	timeout    time.Duration
	maxSize    int64
}

// NewStreamConfig creates a new configuration with validation
func NewStreamConfig(bufferSize, maxSize int64, timeout time.Duration) (*streamConfig, error) {
	if bufferSize <= 0 {
		bufferSize = 32 * 1024 // Default 32KB buffer
	}
	if maxSize < 0 {
		return nil, fmt.Errorf("%w: negative max size", ErrInvalidConfig)
	}
	if timeout < 0 {
		return nil, fmt.Errorf("%w: negative timeout", ErrInvalidConfig)
	}

	return &streamConfig{
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

// stream handles streaming data from a reader
type stream struct {
	reader    io.ReadCloser
	config    *streamConfig
	bytesRead int64
	ctx       context.Context
}

// NewStream creates a new stream with configuration
func NewStream(ctx context.Context, reader io.ReadCloser, cfg *streamConfig) (*stream, error) {
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

	return &stream{
		reader: reader,
		config: cfg,
		ctx:    ctx,
	}, nil
}

// ReadChunk reads the next chunk of data
func (s *stream) ReadChunk() ([]byte, error) {
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
		return nil, err
	}

	s.bytesRead += int64(len(data))
	return data, nil
}

func (s *stream) checkMaxSize() error {
	if s.config.maxSize > 0 && s.bytesRead >= s.config.maxSize {
		return ErrMaxSizeExceeded
	}
	return nil
}

func (s *stream) readDirect(buffer []byte) ([]byte, error) {
	n, err := s.reader.Read(buffer)
	if err != nil {
		return nil, fmt.Errorf("direct read error: %w", err)
	}
	return buffer[:n], nil
}

func (s *stream) readWithTimeout(buffer []byte) ([]byte, error) {
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

func (s *stream) BytesRead() int64 {
	return s.bytesRead
}

func (s *stream) Close() error {
	if err := s.reader.Close(); err != nil {
		return fmt.Errorf("stream close error: %w", err)
	}
	return nil
}
