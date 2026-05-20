// SPDX-License-Identifier: MPL-2.0

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

	"github.com/wippyai/runtime/api/cluster"
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
	Err    error
	Reason ExitReason
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
	// protocolVersion v2 added the per-frame Class byte so the wire is
	// self-describing for sub-protocol dispatch. v1 (header [version, len])
	// is no longer accepted; all nodes in a cluster upgrade together.
	protocolVersion = 0x02
	frameHeaderSize = 6 // 1 byte version, 1 byte class, 4 bytes length
	// writeBatchSize reserved for future use
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

// Outbound is one queued message waiting to be sent on the wire. The Class
// is preserved end-to-end so the receiver can dispatch by sub-protocol
// without inspecting the payload, and so requeue-on-disconnect honors the
// original QoS class.
type Outbound struct {
	Data  []byte
	Class Class
}

// Connection defines the public interface for a connection to another node.
type Connection interface {
	Run(handler func(class Class, msg []byte)) *ConnectionError
	Send(data []byte, class Class) error
	Close()
	ExtractPendingMessages() []Outbound
	RemoteNodeID() cluster.NodeID
}

// NodeConnectionConfig holds configuration parameters for a NodeConnection.
type NodeConnectionConfig struct {
	HandshakeTimeout time.Duration
	MaxMessageSize   uint32
	// MaxQueueSize caps the per-connection outbound queue (activeQueue).
	// When the cap is reached, Send drops the new message and returns
	// ErrQueueFull so the caller can record the drop. Zero means unbounded
	// — that's a foot-gun under network-delay chaos and should only be used
	// in tests.
	MaxQueueSize int
}

// DefaultNodeConnectionConfig returns a default set of configuration parameters
// for NodeConnection with reasonable timeout and size limits.
func DefaultNodeConnectionConfig() NodeConnectionConfig {
	return NodeConnectionConfig{
		HandshakeTimeout: 5 * time.Second,
		MaxMessageSize:   512 * 1024 * 1024,
		MaxQueueSize:     4096,
	}
}

// NodeConnection represents a single, framed, and optimized TCP connection to another node.
// It provides message queuing, batching, and lifecycle management for internode communication.
type NodeConnection struct {
	conn            net.Conn
	logger          *zap.Logger
	cancel          context.CancelFunc
	activeQueue     *list.List
	processingQueue *list.List
	sendNotify      chan struct{}
	remoteNode      cluster.NodeID
	config          NodeConnectionConfig
	lifecycleMu     sync.Mutex
	queueMu         sync.Mutex
	closed          atomic.Bool
}

// newNodeConnection creates a new, un-started NodeConnection.
func newNodeConnection(conn net.Conn, remoteNode cluster.NodeID, config NodeConnectionConfig, logger *zap.Logger) *NodeConnection {
	return &NodeConnection{
		conn:            conn,
		logger:          logger.With(zap.String("remote_node", remoteNode)),
		config:          config,
		remoteNode:      remoteNode,
		activeQueue:     list.New(),
		processingQueue: list.New(),
		sendNotify:      make(chan struct{}, 1),
	}
}

// Run starts the connection's read/write loops and blocks until termination.
// It returns a ConnectionError indicating the reason for termination. The
// handler receives every decoded inbound frame tagged with its sub-protocol
// Class so callers can dispatch by class without re-parsing payload.
func (c *NodeConnection) Run(handler func(class Class, msg []byte)) *ConnectionError {
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
// Returns ErrQueueFull when MaxQueueSize is configured and the activeQueue
// is at capacity — the caller is expected to record the drop and proceed.
// Without this cap, network-delay chaos would let activeQueue grow unbounded
// (each frame up to MaxMessageSize) and OOMKill the runtime.
func (c *NodeConnection) Send(data []byte, class Class) error {
	if c.closed.Load() {
		return ErrConnectionClosed
	}

	c.queueMu.Lock()
	if c.config.MaxQueueSize > 0 && c.activeQueue.Len() >= c.config.MaxQueueSize {
		c.queueMu.Unlock()
		return ErrQueueFull
	}
	c.activeQueue.PushBack(Outbound{Data: data, Class: class})
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
// This is useful for message recovery during connection failures. The original
// Class is preserved on every returned entry so the caller can requeue by
// class on the new connection.
func (c *NodeConnection) ExtractPendingMessages() []Outbound {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	c.processingQueue.PushBackList(c.activeQueue)
	pending := make([]Outbound, 0, c.processingQueue.Len())
	for e := c.processingQueue.Front(); e != nil; e = e.Next() {
		pending = append(pending, e.Value.(Outbound))
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

		// Clear processingQueue under lock to avoid race with ExtractPendingMessages
		c.queueMu.Lock()
		c.processingQueue.Init()
		c.queueMu.Unlock()
	}
}

func (c *NodeConnection) flushBatch(writer *bufio.Writer, batch *list.List) error {
	for e := batch.Front(); e != nil; e = e.Next() {
		msg := e.Value.(Outbound)
		if err := writeFrame(writer, msg.Class, msg.Data); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func (c *NodeConnection) readLoop(ctx context.Context, handler func(class Class, msg []byte)) *ConnectionError {
	reader := bufio.NewReader(c.conn)
	for {
		// Check for context cancellation before blocking on read.
		select {
		case <-ctx.Done():
			return &ConnectionError{Reason: ExitCleanShutdown, Err: ctx.Err()}
		default:
		}

		class, msg, err := readFrame(reader, c.config.MaxMessageSize)

		if err != nil {
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

		// Call handler without logging - this is hot path
		handler(class, msg)
	}
}

// protocolError represents an error in the internode communication protocol.
type protocolError string

// Error implements the error interface for protocolError.
func (e protocolError) Error() string { return "protocol error: " + string(e) }

// writeFrame writes a framed message to the writer with protocol version,
// sub-protocol class, and length prefix.
func writeFrame(w io.Writer, class Class, data []byte) error {
	var header [frameHeaderSize]byte
	header[0] = protocolVersion
	header[1] = byte(class)

	// Check for potential integer overflow before casting to uint32
	dataLen := len(data)
	if dataLen > math.MaxUint32 {
		return NewMessageTooLargeError(dataLen)
	}

	binary.LittleEndian.PutUint32(header[2:], uint32(dataLen))
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

// readFrame reads a framed message from the reader, validating protocol
// version, sub-protocol class, and message size.
func readFrame(r io.Reader, maxMessageSize uint32) (Class, []byte, error) {
	var header [frameHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	if header[0] != protocolVersion {
		return 0, nil, protocolError(fmt.Sprintf("unexpected protocol version %d", header[0]))
	}
	class := Class(header[1])
	if int(class) >= numClasses {
		return 0, nil, protocolError(fmt.Sprintf("unknown sub-protocol class %d", header[1]))
	}
	size := binary.LittleEndian.Uint32(header[2:])
	if size > maxMessageSize {
		return 0, nil, NewMessageSizeExceedsMaxError(int(size), int(maxMessageSize))
	}

	if size == 0 {
		return class, []byte{}, nil
	}

	var msg []byte
	bp := bufferPool.Get().(*[]byte)

	// Check for potential integer overflow before casting to uint32
	bufCap := cap(*bp)
	if bufCap < 0 || bufCap > math.MaxUint32 {
		// This shouldn't happen in practice, but handle it safely
		bufCap = math.MaxUint32
	}

	if int(size) > bufCap {
		msg = make([]byte, size)
	} else {
		msg = (*bp)[:size]
	}

	if _, err := io.ReadFull(r, msg); err != nil {
		bufferPool.Put(bp)
		return 0, nil, err
	}

	// If it's from the pool, we must copy it.
	if &msg[0] == &(*bp)[0] {
		data := make([]byte, size)
		copy(data, msg)
		bufferPool.Put(bp)
		return class, data, nil
	}

	// It was a large message allocated on its own.
	return class, msg, nil
}
