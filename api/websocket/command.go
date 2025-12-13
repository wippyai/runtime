// Package websocket provides WebSocket-related command types for the dispatcher system.
package websocket

import (
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/pid"
)

func init() {
	dispatcher.MustRegisterCommands("ws",
		CmdWsConnect, CmdWsSend, CmdWsReceive, CmdWsClose, CmdWsPing, CmdWsSubscribe,
	)
}

// Command IDs for WebSocket operations.
// Range 80-89 is reserved for WebSocket commands.
const (
	CmdWsConnect   dispatcher.CommandID = 80 // Connect to WebSocket server
	CmdWsSend      dispatcher.CommandID = 81 // Send message
	CmdWsReceive   dispatcher.CommandID = 82 // Receive message (blocking)
	CmdWsClose     dispatcher.CommandID = 83 // Close connection
	CmdWsPing      dispatcher.CommandID = 84 // Send ping
	CmdWsSubscribe dispatcher.CommandID = 85 // Subscribe to messages (returns channel)
)

// Message types for WebSocket frames.
const (
	MessageText   = 1
	MessageBinary = 2
)

// Compression modes for WebSocket connections.
const (
	CompressionDisabled        = 0 // No compression
	CompressionContextTakeover = 1 // Compression with context takeover (more efficient)
	CompressionNoContext       = 2 // Compression without context takeover (less memory)
)

// WsConnectCmd connects to a WebSocket server.
// Returns connection ID via emit on success.
type WsConnectCmd struct {
	URL       string
	Headers   map[string]string // Optional headers
	Protocols []string          // Subprotocols to negotiate

	// Timeout options
	DialTimeout  time.Duration // Dial timeout (0 = default 30s)
	ReadTimeout  time.Duration // Read timeout per message (0 = no timeout)
	WriteTimeout time.Duration // Write timeout per message (0 = no timeout)

	// Compression options
	CompressionMode      int // 0=disabled, 1=context takeover, 2=no context takeover
	CompressionThreshold int // Min message size for compression (0 = default 512)

	// Read limits
	ReadLimit int64 // Max message size in bytes (0 = default 16MB)

	// Channel options
	ChannelCapacity int // Capacity for receive channel (0 = unbuffered)
}

// CmdID implements dispatcher.Command.
func (c WsConnectCmd) CmdID() dispatcher.CommandID {
	return CmdWsConnect
}

// WsSendCmd sends a message on a WebSocket connection.
type WsSendCmd struct {
	ConnID      uint64
	Data        []byte
	MessageType int // MessageText or MessageBinary
}

// CmdID implements dispatcher.Command.
func (c WsSendCmd) CmdID() dispatcher.CommandID {
	return CmdWsSend
}

// WsReceiveCmd receives a message from a WebSocket connection.
// Blocks until a message is available or connection closes.
// Returns WsMessage via emit, or nil on close.
type WsReceiveCmd struct {
	ConnID uint64
}

// CmdID implements dispatcher.Command.
func (c WsReceiveCmd) CmdID() dispatcher.CommandID {
	return CmdWsReceive
}

// WsCloseCmd closes a WebSocket connection.
type WsCloseCmd struct {
	ConnID uint64
	Code   int    // Close code (optional, 0 = normal)
	Reason string // Close reason (optional)
}

// CmdID implements dispatcher.Command.
func (c WsCloseCmd) CmdID() dispatcher.CommandID {
	return CmdWsClose
}

// WsPingCmd sends a ping on the connection.
type WsPingCmd struct {
	ConnID uint64
	Data   []byte
}

// CmdID implements dispatcher.Command.
func (c WsPingCmd) CmdID() dispatcher.CommandID {
	return CmdWsPing
}

// WsMessage represents a received WebSocket message.
type WsMessage struct {
	Data        []byte
	MessageType int  // MessageText or MessageBinary
	EOF         bool // True if connection closed
}

// WsSubscribeCmd starts a background read loop that sends messages to a relay topic.
// The handler spawns a goroutine that reads until connection closes.
// Each message is delivered to the specified topic via relay.
type WsSubscribeCmd struct {
	ConnID uint64
	Topic  string  // Per-connection topic (e.g., "ws@123")
	PID    pid.PID // Target process PID to send messages to
}

// CmdID implements dispatcher.Command.
func (c WsSubscribeCmd) CmdID() dispatcher.CommandID {
	return CmdWsSubscribe
}

// WsSubscription represents an active subscription to WebSocket messages.
type WsSubscription struct {
	ConnID uint64
	Topic  string // The topic messages are sent to
}
