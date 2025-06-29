package internode

import (
	"bufio"
	"container/list"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ponyruntime/pony/api/cluster"
	"go.uber.org/zap"
)

var (
	ErrConnectionClosed = errors.New("internode: connection is closed")
	ErrMessageTooLarge  = errors.New("internode: message exceeds max size")
	ErrCleanShutdown    = errors.New("internode: clean shutdown")
)

// ExitReason defines the category of error that caused a connection to terminate.
type ExitReason int

const (
	ExitUnknown ExitReason = iota
	ExitCleanShutdown
	ExitNetworkError
	ExitProtocolError
	ExitPeerClosed
)

// String returns a human-readable representation of the ExitReason.
func (er ExitReason) String() string {
	switch er {
	case ExitUnknown:
		return "UNKNOWN"
	case ExitCleanShutdown:
		return "CLEAN_SHUTDOWN"
	case ExitNetworkError:
		return "NETWORK_ERROR"
	case ExitProtocolError:
		return "PROTOCOL_ERROR"
	case ExitPeerClosed:
		return "PEER_CLOSED"
	default:
		return "UNKNOWN"
	}
}

// ConnectionError is a structured error returned by a connection's Run loop.
// It provides both the reason for termination and the underlying error.
type ConnectionError struct {
	Reason ExitReason
	Err    error
}

// Error implements the error interface.
func (ce *ConnectionError) Error() string {
	if ce.Err != nil {
		return fmt.Sprintf("%s: %v", ce.Reason, ce.Err)
	}
	return ce.Reason.String()
}

// Unwrap returns the underlying error for error wrapping support.
func (ce *ConnectionError) Unwrap() error { return ce.Err }

// ShouldRetry determines whether the connection should be retried based on the exit reason.
func (ce *ConnectionError) ShouldRetry() bool {
	switch ce.Reason {
	case ExitNetworkError, ExitPeerClosed:
		return true
	case ExitCleanShutdown, ExitProtocolError:
		return false
	case ExitUnknown:
		return false
	default:
		return false
	}
}

const (
	protocolVersion        = 0x01
	frameHeaderSize        = 5 // 1 byte for version, 4 bytes for length
	writeBatchSize         = 128
	writeFlushInterval     = 10 * time.Millisecond
	defaultWriteBufferSize = 64 * 1024
	readPoolBufferSize     = 32 * 1024
)

var bufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, readPoolBufferSize)
		return &b
	},
}

// InternodeConnection defines the public interface for a connection to another node.
type InternodeConnection interface {
	Run(handler func(msg []byte)) *ConnectionError
	Send(data []byte) error
	Close()
	ExtractPendingMessages() [][]byte
	RemoteNodeID() cluster.NodeID
}

// NodeConnectionConfig holds configuration parameters for a NodeConnection.
type NodeConnectionConfig struct {
	HandshakeTimeout time.Duration
	MaxMessageSize   uint32
}

// DefaultNodeConnectionConfig returns a default set of configuration parameters
// for NodeConnection with reasonable timeout and size limits.
func DefaultNodeConnectionConfig() NodeConnectionConfig {
	return NodeConnectionConfig{
		HandshakeTimeout: 5 * time.Second,
		MaxMessageSize:   512 * 1024 * 1024,
	}
}

// NodeConnection represents a single, framed, and optimized TCP connection to another node.
// It provides message queuing, batching, and lifecycle management for internode communication.
type NodeConnection struct {
	conn       net.Conn
	logger     *zap.Logger
	config     NodeConnectionConfig
	remoteNode cluster.NodeID

	lifecycleMu sync.Mutex // Protects access to the cancel function.
	cancel      context.CancelFunc

	closed atomic.Bool

	queueMu         sync.Mutex
	activeQueue     *list.List
	processingQueue *list.List
	sendNotify      chan struct{}
}

// newNodeConnection creates a new, un-started NodeConnection.
func newNodeConnection(conn net.Conn, remoteNode cluster.NodeID, config NodeConnectionConfig, logger *zap.Logger) *NodeConnection {
	return &NodeConnection{
		conn:            conn,
		logger:          logger.With(zap.String("remote_node", string(remoteNode))),
		config:          config,
		remoteNode:      remoteNode,
		activeQueue:     list.New(),
		processingQueue: list.New(),
		sendNotify:      make(chan struct{}, 1),
	}
}

// Run starts the connection's read/write loops and blocks until termination.
// It returns a ConnectionError indicating the reason for termination.
func (c *NodeConnection) Run(handler func(msg []byte)) *ConnectionError {
	c.lifecycleMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.lifecycleMu.Unlock()

	defer c.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	errChan := make(chan *ConnectionError, 2)

	go func() {
		defer wg.Done()
		err := c.readLoop(ctx, handler)
		// Only send non-nil errors that aren't clean shutdowns
		if err != nil && (err.Reason != ExitCleanShutdown) {
			errChan <- err
		}
	}()

	go func() {
		defer wg.Done()
		err := c.writeLoop(ctx)
		// Only send non-nil errors that aren't clean shutdowns
		if err != nil && (err.Reason != ExitCleanShutdown) {
			errChan <- err
		}
	}()

	var firstErr *ConnectionError
	select {
	case firstErr = <-errChan:
		// First error received, initiate shutdown of the other loop.
		c.Close()
	case <-ctx.Done():
		// Shutdown initiated externally via Close().
		firstErr = &ConnectionError{Reason: ExitCleanShutdown, Err: ErrCleanShutdown}
	}

	wg.Wait()
	return firstErr
}

// Send enqueues a message for delivery. It is non-blocking and safe for concurrent use.
// Returns ErrConnectionClosed if the connection has been closed.
func (c *NodeConnection) Send(data []byte) error {
	if c.closed.Load() {
		return ErrConnectionClosed
	}

	c.queueMu.Lock()
	c.activeQueue.PushBack(data)
	c.queueMu.Unlock()

	select {
	case c.sendNotify <- struct{}{}:
	default:
	}

	return nil
}

// Close terminates the connection gracefully and cancels all ongoing operations.
func (c *NodeConnection) Close() {
	if c.closed.CompareAndSwap(false, true) {
		c.lifecycleMu.Lock()
		if c.cancel != nil {
			c.cancel()
		}
		c.lifecycleMu.Unlock()
		_ = c.conn.Close()
	}
}

// RemoteNodeID returns the identifier of the connected peer node.
func (c *NodeConnection) RemoteNodeID() cluster.NodeID {
	return c.remoteNode
}

// ExtractPendingMessages returns all messages that were queued but not yet sent.
// This is useful for message recovery during connection failures.
func (c *NodeConnection) ExtractPendingMessages() [][]byte {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	c.processingQueue.PushBackList(c.activeQueue)
	pending := make([][]byte, 0, c.processingQueue.Len())
	for e := c.processingQueue.Front(); e != nil; e = e.Next() {
		pending = append(pending, e.Value.([]byte))
	}
	return pending
}

func (c *NodeConnection) writeLoop(ctx context.Context) *ConnectionError {
	writer := bufio.NewWriterSize(c.conn, defaultWriteBufferSize)
	ticker := time.NewTicker(writeFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.sendNotify:
		case <-ticker.C:
		case <-ctx.Done():
			return &ConnectionError{Reason: ExitCleanShutdown, Err: ErrCleanShutdown}
		}

		c.queueMu.Lock()
		if c.activeQueue.Len() == 0 {
			c.queueMu.Unlock()
			continue
		}

		c.processingQueue = c.activeQueue
		c.activeQueue = list.New()
		c.queueMu.Unlock()

		if err := c.flushBatch(writer, c.processingQueue); err != nil {
			return &ConnectionError{Reason: ExitNetworkError, Err: err}
		}
	}
}

func (c *NodeConnection) flushBatch(writer *bufio.Writer, batch *list.List) error {
	for e := batch.Front(); e != nil; e = e.Next() {
		if err := writeFrame(writer, e.Value.([]byte)); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	batch.Init()
	return nil
}

func (c *NodeConnection) readLoop(ctx context.Context, handler func(msg []byte)) *ConnectionError {
	reader := bufio.NewReader(c.conn)
	for {
		// Check for context cancellation before blocking on read.
		select {
		case <-ctx.Done():
			return &ConnectionError{Reason: ExitCleanShutdown, Err: ctx.Err()}
		default:
		}

		msg, poolBuf, err := readFrame(reader, c.config.MaxMessageSize)

		if err != nil {
			// Return pool buffer on error
			if poolBuf != nil {
				bufferPool.Put(poolBuf)
			}

			if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "use of closed network connection") {
				// The connection was closed. Check if it was because of our own context.
				select {
				case <-ctx.Done():
					return &ConnectionError{Reason: ExitCleanShutdown, Err: ErrCleanShutdown}
				default:
					return &ConnectionError{Reason: ExitPeerClosed, Err: err}
				}
			}

			if errors.Is(err, ErrMessageTooLarge) {
				return &ConnectionError{Reason: ExitProtocolError, Err: err}
			}

			var perr protocolError
			if errors.As(err, &perr) {
				return &ConnectionError{Reason: ExitProtocolError, Err: perr}
			}
			return &ConnectionError{Reason: ExitNetworkError, Err: err}
		}

		// Call handler - must use data immediately as it may be from pool
		handler(msg)

		// Return pool buffer after handler completes
		if poolBuf != nil {
			bufferPool.Put(poolBuf)
		}
	}
}

// protocolError represents an error in the internode communication protocol.
type protocolError string

// Error implements the error interface for protocolError.
func (e protocolError) Error() string { return "protocol error: " + string(e) }

// writeFrame writes a framed message to the writer with protocol version and length prefix.
func writeFrame(w io.Writer, data []byte) error {
	var header [frameHeaderSize]byte
	header[0] = protocolVersion

	// Check for potential integer overflow before casting to uint32
	dataLen := len(data)
	if dataLen > math.MaxUint32 {
		return fmt.Errorf("message too large: %d bytes exceeds maximum uint32", dataLen)
	}

	binary.LittleEndian.PutUint32(header[1:], uint32(dataLen))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if len(data) > 0 {
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// readFrame reads a framed message from the reader, returning both the message data
// and an optional pool buffer that must be returned after use.
// Returns: (message_data, pool_buffer_reference, error)
// If pool_buffer_reference is non-nil, caller MUST return it to bufferPool.
func readFrame(r io.Reader, maxMessageSize uint32) ([]byte, *[]byte, error) {
	var header [frameHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, nil, err
	}
	if header[0] != protocolVersion {
		return nil, nil, protocolError(fmt.Sprintf("unexpected protocol version %d", header[0]))
	}
	size := binary.LittleEndian.Uint32(header[1:])
	if size > maxMessageSize {
		return nil, nil, fmt.Errorf("%w: message size %d exceeds max %d", ErrMessageTooLarge, size, maxMessageSize)
	}

	if size == 0 {
		return []byte{}, nil, nil
	}

	bp := bufferPool.Get().(*[]byte)

	// Check for potential integer overflow before casting to uint32
	bufCap := cap(*bp)
	if bufCap > math.MaxUint32 {
		bufCap = math.MaxUint32
	}

	if size > uint32(bufCap) {
		// Message larger than pool buffer, allocate directly and return pool buffer
		bufferPool.Put(bp)
		msg := make([]byte, size)
		if _, err := io.ReadFull(r, msg); err != nil {
			return nil, nil, err
		}
		return msg, nil, nil // No pool buffer to return
	}

	// Use pool buffer directly
	msg := (*bp)[:size]
	if _, err := io.ReadFull(r, msg); err != nil {
		bufferPool.Put(bp)
		return nil, nil, err
	}

	// Return buffer data and pool reference - caller owns pool lifecycle
	return msg, bp, nil
}
