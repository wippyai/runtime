package internode

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Protocol constants
const (
	ProtocolVersion = 0x01
	FrameHeaderSize = 6 // 2 bytes version/flags + 4 bytes length

	// Protocol flags
	FlagReserved = 0x00
)

// NodeConnection represents a connection to a remote node
type NodeConnection struct {
	conn         net.Conn
	localNodeID  string
	remoteNodeID string
	logger       *zap.Logger
	config       Config

	// Channels for async communication
	outbound chan []byte
	closed   chan struct{}

	// Connection state
	mu     sync.RWMutex
	active bool
}

// NewNodeConnection creates a new node connection
func NewNodeConnection(conn net.Conn, localNodeID, remoteNodeID string, config Config, logger *zap.Logger) *NodeConnection {
	return &NodeConnection{
		conn:         conn,
		localNodeID:  localNodeID,
		remoteNodeID: remoteNodeID,
		config:       config,
		logger:       logger.With(zap.String("remote_node", remoteNodeID)),
		outbound:     make(chan []byte, config.OutboundQueueSize),
		closed:       make(chan struct{}),
		active:       true,
	}
}

// Start begins handling the connection lifecycle
func (nc *NodeConnection) Start(ctx context.Context, onMessage func(string, []byte)) {
	defer nc.cleanup()

	var wg sync.WaitGroup

	// Start reader goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		nc.readLoop(ctx, onMessage)
	}()

	// Start writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		nc.writeLoop(ctx)
	}()

	// Wait for context cancellation or connection closure
	select {
	case <-ctx.Done():
		nc.logger.Debug("connection closed due to context cancellation")
	case <-nc.closed:
		nc.logger.Debug("connection closed")
	}

	wg.Wait()
}

// Send queues data for sending
func (nc *NodeConnection) Send(data []byte) error {
	nc.mu.RLock()
	defer nc.mu.RUnlock()

	if !nc.active {
		return fmt.Errorf("connection to %s is closed", nc.remoteNodeID)
	}

	select {
	case nc.outbound <- data:
		return nil
	default:
		return fmt.Errorf("outbound queue full for node %s", nc.remoteNodeID)
	}
}

// Close closes the connection
func (nc *NodeConnection) Close() {
	nc.mu.Lock()
	defer nc.mu.Unlock()

	if nc.active {
		nc.active = false
		close(nc.closed)
		nc.conn.Close()
	}
}

// PerformHandshake handles the initial handshake with the remote node
func (nc *NodeConnection) PerformHandshake(isOutbound bool, expectedNodeID string) error {
	// Set handshake timeout
	deadline := time.Now().Add(nc.config.HandshakeTimeout)
	nc.conn.SetDeadline(deadline)
	defer nc.conn.SetDeadline(time.Time{}) // Clear deadline

	if isOutbound {
		return nc.performOutboundHandshake(expectedNodeID)
	}
	return nc.performInboundHandshake()
}

// performOutboundHandshake handles outbound connection handshake
func (nc *NodeConnection) performOutboundHandshake(expectedNodeID string) error {
	// Send our node ID
	if err := nc.sendFrame([]byte(nc.localNodeID)); err != nil {
		return fmt.Errorf("failed to send local node ID: %w", err)
	}

	// Receive their node ID
	data, err := nc.readFrame()
	if err != nil {
		return fmt.Errorf("failed to receive remote node ID: %w", err)
	}

	remoteNodeID := string(data)
	if remoteNodeID != expectedNodeID {
		return fmt.Errorf("node ID mismatch: expected %s, got %s", expectedNodeID, remoteNodeID)
	}

	// Update our stored remote node ID
	nc.remoteNodeID = remoteNodeID
	return nil
}

// performInboundHandshake handles inbound connection handshake
func (nc *NodeConnection) performInboundHandshake() error {
	// Receive their node ID
	data, err := nc.readFrame()
	if err != nil {
		return fmt.Errorf("failed to receive remote node ID: %w", err)
	}

	// Update our stored remote node ID
	nc.remoteNodeID = string(data)

	// Send our node ID
	if err := nc.sendFrame([]byte(nc.localNodeID)); err != nil {
		return fmt.Errorf("failed to send local node ID: %w", err)
	}

	return nil
}

// readLoop handles incoming messages
func (nc *NodeConnection) readLoop(ctx context.Context, onMessage func(string, []byte)) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-nc.closed:
			return
		default:
			// Read message from connection
			data, err := nc.readFrame()
			if err != nil {
				nc.logger.Error("failed to read message", zap.Error(err))
				nc.Close()
				return
			}

			// Handle message
			onMessage(nc.remoteNodeID, data)
		}
	}
}

// writeLoop handles outgoing messages
func (nc *NodeConnection) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-nc.closed:
			return
		case data := <-nc.outbound:
			// Send message
			if err := nc.sendFrame(data); err != nil {
				nc.logger.Error("failed to write message", zap.Error(err))
				nc.Close()
				return
			}
		}
	}
}

// sendFrame writes a frame to the connection
func (nc *NodeConnection) sendFrame(data []byte) error {
	header := make([]byte, FrameHeaderSize)
	header[0] = ProtocolVersion                               // Version
	header[1] = FlagReserved                                  // Flags
	binary.BigEndian.PutUint32(header[2:], uint32(len(data))) // Length

	// Write header
	if _, err := nc.conn.Write(header); err != nil {
		return fmt.Errorf("failed to write frame header: %w", err)
	}

	// Write data
	if len(data) > 0 {
		if _, err := nc.conn.Write(data); err != nil {
			return fmt.Errorf("failed to write frame data: %w", err)
		}
	}

	return nil
}

// readFrame reads a frame from the connection
func (nc *NodeConnection) readFrame() ([]byte, error) {
	// Read header
	header := make([]byte, FrameHeaderSize)
	if _, err := io.ReadFull(nc.conn, header); err != nil {
		return nil, fmt.Errorf("failed to read frame header: %w", err)
	}

	// Check version
	version := header[0]
	if version != ProtocolVersion {
		return nil, fmt.Errorf("unsupported protocol version: %d", version)
	}

	// Extract length
	length := binary.BigEndian.Uint32(header[2:])
	if length == 0 {
		return []byte{}, nil
	}

	// Read data
	data := make([]byte, length)
	if _, err := io.ReadFull(nc.conn, data); err != nil {
		return nil, fmt.Errorf("failed to read frame data: %w", err)
	}

	return data, nil
}

// cleanup performs connection cleanup
func (nc *NodeConnection) cleanup() {
	nc.Close()
	nc.logger.Debug("connection cleanup completed")
}
