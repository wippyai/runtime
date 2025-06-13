package internode

import (
	"bufio"
	"container/list"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ponyruntime/pony/api/cluster"
	"go.uber.org/zap"
)

var (
	// ErrConnectionClosed is returned when an operation is attempted on a closed connection.
	ErrConnectionClosed = errors.New("internode: connection is closed")
	// ErrMessageTooLarge is returned when a send is attempted on a message that exceeds the configured max size.
	ErrMessageTooLarge = errors.New("internode: message exceeds max size")
)

// Constants for the framing protocol and I/O optimization.
const (
	// protocolVersion is a single byte prefixed to each frame to ensure
	// compatibility between different versions of the service.
	protocolVersion = 0x01

	// frameHeaderSize defines the total size of the frame header.
	// It consists of 1 byte for the version and 4 bytes for the message length.
	frameHeaderSize = 5

	// writeBatchSize is the number of messages to accumulate in the local
	// outbound channel before forcing a flush to the network socket. This
	// reduces the number of syscalls for high-frequency small messages.
	writeBatchSize = 128

	// writeFlushInterval is a timeout that ensures that even if the batch
	// is not full, messages are sent promptly. It prevents high latency for
	// lone messages in an otherwise idle connection.
	writeFlushInterval = 10 * time.Millisecond
)

// bufferPool is a pool of byte buffers used for reading data from the network.
var bufferPool = sync.Pool{
	New: func() any {
		// Buffers are sized to handle a reasonably large message without
		// requiring a new allocation. 32KB is a common default.
		b := make([]byte, 32*1024)
		return &b
	},
}

// NodeConnectionConfig contains configuration specific to a single NodeConnection.
// This avoids coupling NodeConnection to the broader ManagerConfig.
type NodeConnectionConfig struct {
	HandshakeTimeout  time.Duration
	OutboundQueueSize int
	MaxMessageSize    uint32
}

// DefaultNodeConnectionConfig returns sensible defaults for NodeConnection.
func DefaultNodeConnectionConfig() NodeConnectionConfig {
	return NodeConnectionConfig{
		HandshakeTimeout:  5 * time.Second,
		OutboundQueueSize: 256,
		MaxMessageSize:    512 * 1024 * 1024, // 512MB
	}
}

// NodeConnection represents a single, framed, and optimized TCP connection to another node.
// It follows Erlang-like semantics with unbounded message queuing to prevent blocking.
type NodeConnection struct {
	conn         net.Conn
	logger       *zap.Logger
	config       NodeConnectionConfig
	remoteNodeID cluster.NodeID

	// outbound is the primary buffered channel for messages
	outbound chan []byte

	// overflow handles messages when the main channel is full (Erlang-style unbounded queue)
	overflow   *list.List
	overflowMu sync.Mutex

	// shutdown is used for an immediate, ungraceful signal to loops.
	shutdown chan struct{}

	// closeOnce ensures that the cleanup logic in Close() is executed exactly once.
	closeOnce sync.Once

	// isClosed provides thread-safe, lock-free access to the connection's state.
	isClosed atomic.Bool
}

// newNodeConnection creates a new, unstarted NodeConnection.
// The connection's lifecycle is managed by the context passed to Run().
func newNodeConnection(conn net.Conn, remoteNodeID cluster.NodeID, config NodeConnectionConfig, logger *zap.Logger) *NodeConnection {
	// Create a properly named logger for this connection
	connLogger := logger.Named("connection").With(zap.String("remote_node", string(remoteNodeID)))

	return &NodeConnection{
		conn:         conn,
		remoteNodeID: remoteNodeID,
		config:       config,
		logger:       connLogger,
		outbound:     make(chan []byte, config.OutboundQueueSize),
		overflow:     list.New(),
		shutdown:     make(chan struct{}),
	}
}

// Run starts the read and write loops for the connection. This method is
// BLOCKING and will not return until the connection is fully terminated.
// The provided context controls the connection's lifecycle.
func (nc *NodeConnection) Run(ctx context.Context, onMessage func(nodeID cluster.NodeID, data []byte)) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		nc.writeLoop(ctx)
	}()
	go func() {
		defer wg.Done()
		nc.readLoop(ctx, onMessage)
	}()

	wg.Wait()
}

// Send queues a data frame to be sent to the remote node. It follows Erlang semantics:
// - Never blocks (uses overflow queue when main buffer is full)
// - Never drops messages (unbounded queue like Erlang mailbox)
// - Fails only if connection is already closed
func (nc *NodeConnection) Send(data []byte) error {
	if nc.isClosed.Load() {
		return ErrConnectionClosed
	}

	select {
	case nc.outbound <- data:
		return nil
	case <-nc.shutdown:
		return ErrConnectionClosed
	default:
		// Main buffer is full - use overflow (Erlang-style unbounded growth)
		nc.overflowMu.Lock()
		nc.overflow.PushBack(data)
		nc.overflowMu.Unlock()
		return nil
	}
}

// ExtractPendingMessages atomically extracts all undelivered messages from the connection.
// This is used when a connection fails to recover messages for requeuing.
// Returns messages in the order they were queued (oldest first).
func (nc *NodeConnection) ExtractPendingMessages() [][]byte {
	var messages [][]byte

	// First, drain the main channel (non-blocking)
	for {
		select {
		case msg := <-nc.outbound:
			messages = append(messages, msg)
		default:
			goto drainOverflow
		}
	}

drainOverflow:
	// Then drain the overflow queue
	nc.overflowMu.Lock()
	for nc.overflow.Len() > 0 {
		elem := nc.overflow.Front()
		if elem == nil {
			break
		}

		if data, ok := elem.Value.([]byte); ok {
			messages = append(messages, data)
		}
		nc.overflow.Remove(elem)
	}
	nc.overflowMu.Unlock()

	return messages
}

// Close gracefully shuts down the connection and its associated goroutines.
// This method is thread-safe and idempotent.
func (nc *NodeConnection) Close() {
	nc.closeOnce.Do(func() {
		if nc.isClosed.CompareAndSwap(false, true) {
			close(nc.shutdown)
			if err := nc.conn.Close(); err != nil && !isErrClosing(err) {
				nc.logger.Warn("Error closing connection", zap.Error(err))
			}
		}
	})
}

// RemoteNodeID returns the identifier of the node on the other end of the connection.
func (nc *NodeConnection) RemoteNodeID() cluster.NodeID {
	return nc.remoteNodeID
}

// readLoop continuously reads data from the socket. It uses a buffered reader to minimize
// syscalls and a buffer pool to minimize memory allocations.
func (nc *NodeConnection) readLoop(ctx context.Context, onMessage func(nodeID cluster.NodeID, data []byte)) {
	defer nc.Close()
	reader := bufio.NewReader(nc.conn)

	for {
		select {
		case <-ctx.Done():
			return
		case <-nc.shutdown:
			return
		default:
			data, err := nc.readFrame(reader)
			if err != nil {
				if err != io.EOF && !isErrClosing(err) {
					nc.logger.Error("Failed to read frame", zap.Error(err))
				}
				return
			}

			onMessage(nc.remoteNodeID, data)
			bufferPool.Put(&data)
		}
	}
}

// writeLoop continuously drains the outbound channel and overflow queue, writing data to the socket.
// It batches writes together to improve performance and reduce syscalls.
func (nc *NodeConnection) writeLoop(ctx context.Context) {
	defer nc.Close()
	writer := bufio.NewWriter(nc.conn)
	ticker := time.NewTicker(writeFlushInterval)
	defer ticker.Stop()

	batch := make([][]byte, 0, writeBatchSize)

	for {
		select {
		case data := <-nc.outbound:
			batch = append(batch, data)
			nc.drainOverflowToBatch(&batch)

			if len(batch) >= writeBatchSize {
				if err := nc.flushBatch(writer, &batch); err != nil {
					return
				}
			}
		case <-ticker.C:
			nc.drainOverflowToBatch(&batch)
			if len(batch) > 0 {
				if err := nc.flushBatch(writer, &batch); err != nil {
					return
				}
			}
		case <-ctx.Done():
			if len(batch) > 0 {
				_ = nc.flushBatch(writer, &batch)
			}
			return
		case <-nc.shutdown:
			if len(batch) > 0 {
				_ = nc.flushBatch(writer, &batch)
			}
			return
		}
	}
}

// drainOverflowToBatch moves messages from overflow queue to the current batch
func (nc *NodeConnection) drainOverflowToBatch(batch *[][]byte) {
	nc.overflowMu.Lock()
	defer nc.overflowMu.Unlock()

	remainingCapacity := writeBatchSize - len(*batch)
	if remainingCapacity <= 0 || nc.overflow.Len() == 0 {
		return
	}

	drained := 0
	for drained < remainingCapacity && nc.overflow.Len() > 0 {
		elem := nc.overflow.Front()
		if elem == nil {
			break
		}

		data, ok := elem.Value.([]byte)
		if !ok {
			nc.overflow.Remove(elem)
			continue
		}

		*batch = append(*batch, data)
		nc.overflow.Remove(elem)
		drained++
	}
}

// flushBatch writes all data in the batch slice to the buffered writer and flushes it to the network.
func (nc *NodeConnection) flushBatch(writer *bufio.Writer, batch *[][]byte) error {
	if len(*batch) == 0 {
		return nil
	}

	for _, data := range *batch {
		if err := nc.writeFrame(writer, data); err != nil {
			if !isErrClosing(err) {
				nc.logger.Error("Failed to write frame", zap.Error(err))
			}
			return err
		}
	}

	if err := writer.Flush(); err != nil {
		if !isErrClosing(err) {
			nc.logger.Error("Failed to flush writer", zap.Error(err))
		}
		return err
	}

	*batch = (*batch)[:0]
	return nil
}

// writeFrame prepends the protocol header to the data and writes the full frame to the writer.
func (nc *NodeConnection) writeFrame(writer io.Writer, data []byte) error {
	dataLen := uint32(len(data))
	if dataLen > nc.config.MaxMessageSize {
		return ErrMessageTooLarge
	}

	header := [frameHeaderSize]byte{protocolVersion}
	binary.LittleEndian.PutUint32(header[1:], dataLen)

	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	if dataLen > 0 {
		if _, err := writer.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// readFrame reads a single, complete data frame from the buffered reader.
func (nc *NodeConnection) readFrame(reader *bufio.Reader) ([]byte, error) {
	header := [frameHeaderSize]byte{}
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}

	if header[0] != protocolVersion {
		return nil, fmt.Errorf("unsupported protocol version: %d", header[0])
	}

	length := binary.LittleEndian.Uint32(header[1:])
	if length > nc.config.MaxMessageSize {
		return nil, fmt.Errorf("frame size %d exceeds max allowed size %d", length, nc.config.MaxMessageSize)
	}
	if length == 0 {
		return []byte{}, nil
	}

	bufPtr := bufferPool.Get().(*[]byte)
	data := *bufPtr
	if uint32(len(data)) < length {
		data = make([]byte, length)
		bufPtr = &data
	} else {
		data = data[:length]
	}

	if _, err := io.ReadFull(reader, data); err != nil {
		bufferPool.Put(bufPtr)
		return nil, err
	}
	return data, nil
}

// performHandshake handles the initial protocol exchange to verify node identities.
// It operates with its own deadline, independent of the connection's main context.
func (nc *NodeConnection) performHandshake(ctx context.Context, localNodeID cluster.NodeID, isInitiator bool) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, nc.config.HandshakeTimeout)
		defer cancel()
		deadline = time.Now().Add(nc.config.HandshakeTimeout)
	}

	if err := nc.conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("failed to set connection deadline: %w", err)
	}

	defer func() {
		if err := nc.conn.SetDeadline(time.Time{}); err != nil {
			nc.logger.Warn("Failed to clear connection deadline", zap.Error(err))
		}
	}()

	reader := bufio.NewReader(nc.conn)
	writer := bufio.NewWriter(nc.conn)

	if isInitiator {
		if err := nc.writeFrame(writer, []byte(localNodeID)); err != nil {
			return fmt.Errorf("failed to send local node ID: %w", err)
		}
		if err := writer.Flush(); err != nil {
			return fmt.Errorf("failed to flush local node ID: %w", err)
		}

		data, err := nc.readFrame(reader)
		if err != nil {
			return fmt.Errorf("failed to receive remote node ID: %w", err)
		}

		receivedNodeID := cluster.NodeID(data)
		if receivedNodeID != nc.remoteNodeID {
			return fmt.Errorf("handshake node ID mismatch: expected %s, got %s", nc.remoteNodeID, receivedNodeID)
		}
	} else {
		data, err := nc.readFrame(reader)
		if err != nil {
			return fmt.Errorf("failed to receive remote node ID: %w", err)
		}

		nc.remoteNodeID = cluster.NodeID(data)
		// Update logger with the now-known remote node ID
		nc.logger = nc.logger.With(zap.String("remote_node", string(nc.remoteNodeID)))

		if err := nc.writeFrame(writer, []byte(localNodeID)); err != nil {
			return fmt.Errorf("failed to send local node ID: %w", err)
		}
		if err := writer.Flush(); err != nil {
			return fmt.Errorf("failed to flush local node ID: %w", err)
		}
	}

	return nil
}

// isErrClosing checks if a network error is a "connection closed" error.
// This is used to suppress warnings for expected errors during shutdown.
func isErrClosing(err error) bool {
	return errors.Is(err, net.ErrClosed)
}
