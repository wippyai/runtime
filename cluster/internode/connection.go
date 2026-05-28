// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"bufio"
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
	protocolVersion        = 0x02
	frameHeaderSize        = 6 // 1 byte version, 1 byte class, 4 bytes length
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

// NodeConnection represents a single, framed TCP connection to another node.
// Its writeLoop drains the peer's per-class outbound queues directly (wired
// via bindDrain) so there is a single queue and a single goroutine wakeup
// per frame on the send path.
type NodeConnection struct {
	conn          net.Conn
	logger        *zap.Logger
	cancel        context.CancelFunc
	messageNotify <-chan struct{}
	drainFn       func(int) []Outbound
	requeueFn     func([]Outbound)
	remoteNode    cluster.NodeID
	config        NodeConnectionConfig
	drainBatch    int
	lifecycleMu   sync.Mutex
	closed        atomic.Bool
}

// newNodeConnection creates a new, un-started NodeConnection. bindDrain must
// be called before Run to wire the outbound queue source.
func newNodeConnection(conn net.Conn, remoteNode cluster.NodeID, config NodeConnectionConfig, logger *zap.Logger) *NodeConnection {
	return &NodeConnection{
		conn:       conn,
		logger:     logger.With(zap.String("remote_node", remoteNode)),
		config:     config,
		remoteNode: remoteNode,
	}
}

// bindDrain wires the per-class outbound queues that writeLoop drains. It
// MUST be called before Run. notify is the peer's message notifier (signaled
// when a message is queued); drain pulls up to batch messages in QoS order;
// requeue returns an un-flushed batch to the per-class queues after a write
// failure so a subsequent connection can deliver them.
func (c *NodeConnection) bindDrain(notify <-chan struct{}, drain func(int) []Outbound, requeue func([]Outbound), batch int) {
	c.messageNotify = notify
	c.drainFn = drain
	c.requeueFn = requeue
	c.drainBatch = batch
}

// Run starts the connection's read/write loops and blocks until termination.
// It returns a ConnectionError indicating the reason for termination. The
// handler receives every decoded inbound frame tagged with its sub-protocol
// Class so callers can dispatch by class without re-parsing payload.
// Run drives the connection's full-duplex I/O until either direction fails
// or Close is called, then returns the first non-clean error observed.
//
// The read pump runs INLINE on the caller's goroutine; only the write pump
// is spawned. This keeps full duplex (separate read/write goroutines) while
// removing the dedicated join/wait goroutine the old two-goroutine-plus-parent
// shape required — the caller's goroutine IS the read pump. Net effect:
// 3 long-lived goroutines per connected peer (control loop + this read pump
// + the write pump) instead of 4.
//
// First-error-wins teardown: whichever pump fails first records its error and
// Close()s the connection (cancel ctx + close net.Conn), which unblocks the
// other pump (context cancellation alone does not interrupt a blocking
// net.Conn.Read — only closing the conn does). The writer records its error
// BEFORE calling Close so a writer-originated failure is captured before the
// socket close wakes the inline reader; the reader then observes
// ExitCleanShutdown and Run returns the writer's error.
func (c *NodeConnection) Run(handler func(class Class, msg []byte)) *ConnectionError {
	c.lifecycleMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.lifecycleMu.Unlock()

	defer c.Close()

	errCh := make(chan *ConnectionError, 1)
	writeDone := make(chan struct{})

	// record keeps only the first non-clean error. The one-slot buffered
	// channel + non-blocking send means the loser of the race silently
	// drops its (necessarily clean-shutdown-shaped) error.
	record := func(err *ConnectionError) {
		if err == nil || err.Reason == ExitCleanShutdown {
			return
		}
		select {
		case errCh <- err:
		default:
		}
	}

	go func() {
		defer close(writeDone)
		if err := c.writeLoop(ctx); err != nil && err.Reason != ExitCleanShutdown {
			record(err) // record before Close so it wins over the reader's clean-shutdown
			c.Close()   // cancels ctx + closes net.Conn, unblocking the inline reader
		}
	}()

	// Inline read pump on the caller's goroutine.
	readErr := c.readLoop(ctx, handler)
	// Attribute the reader's error only if the reader is the FIRST cause:
	// if the connection is already closed when the reader exits, the
	// socket error is a consequence of an external Close() or a
	// writer-triggered teardown (which already recorded its own cause),
	// not an independent failure. This preserves the old contract where
	// an external Close yields ExitCleanShutdown.
	if !c.closed.Load() {
		record(readErr)
	}

	c.Close() // idempotent; stops the writer after the reader exits
	<-writeDone

	select {
	case err := <-errCh:
		return err
	default:
		return &ConnectionError{Reason: ExitCleanShutdown, Err: ErrCleanShutdown}
	}
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

// writeLoop drains this peer's per-class outbound queues (wired by bindDrain)
// and flushes frames to the socket. It first drains everything currently
// queued — including messages buffered while the connection was down — then
// blocks on messageNotify for the next batch. On a write failure the
// un-flushed batch is requeued so a subsequent connection can deliver it.
func (c *NodeConnection) writeLoop(ctx context.Context) *ConnectionError {
	if c.drainFn == nil || c.messageNotify == nil {
		// bindDrain was not called — a programmer error. Park on ctx so the
		// connection still tears down cleanly rather than panicking.
		c.logger.Error("writeLoop started without a bound drain source")
		<-ctx.Done()
		return &ConnectionError{Reason: ExitCleanShutdown, Err: ErrCleanShutdown}
	}

	writer := bufio.NewWriterSize(c.conn, defaultWriteBufferSize)

	for {
		for ctx.Err() == nil {
			batch := c.drainFn(c.drainBatch)
			if len(batch) == 0 {
				break
			}
			if err := c.flushBatch(writer, batch); err != nil {
				c.requeueFn(batch)
				// A flush error caused by an intentional local close (read
				// pump failed first, or external Close) must not be reported
				// as a writer-originated network error — that would override
				// the real first cause in Run's first-error-wins contract.
				if ctx.Err() != nil || c.closed.Load() {
					return &ConnectionError{Reason: ExitCleanShutdown, Err: ErrCleanShutdown}
				}
				return &ConnectionError{Reason: ExitNetworkError, Err: err}
			}
			if len(batch) < c.drainBatch {
				break
			}
		}

		select {
		case <-c.messageNotify:
		case <-ctx.Done():
			return &ConnectionError{Reason: ExitCleanShutdown, Err: ErrCleanShutdown}
		}
	}
}

// flushBatch writes every frame in batch to the buffered writer and flushes.
// On any error it returns immediately; the caller requeues the whole batch
// (no frame in a failed batch is guaranteed delivered).
func (c *NodeConnection) flushBatch(writer *bufio.Writer, batch []Outbound) error {
	for _, msg := range batch {
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
