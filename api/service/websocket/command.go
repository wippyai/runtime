// SPDX-License-Identifier: MPL-2.0

// Package websocket provides WebSocket-related command types for the dispatcher system.
package websocket

import (
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/pid"
)

func init() {
	dispatcher.MustRegisterCommands("ws",
		Connect, Send, Receive, Close, Ping, Subscribe,
	)
}

// Command IDs for WebSocket operations.
// Range 80-89 is reserved for WebSocket commands.
const (
	Connect   dispatcher.CommandID = 80 // Connect to WebSocket server
	Send      dispatcher.CommandID = 81 // Send message
	Receive   dispatcher.CommandID = 82 // Receive message (blocking)
	Close     dispatcher.CommandID = 83 // Close connection
	Ping      dispatcher.CommandID = 84 // Send ping
	Subscribe dispatcher.CommandID = 85 // Subscribe to messages (returns channel)
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

// ConnectCmd connects to a WebSocket server.
// Returns connection ID via emit on success.
type ConnectCmd struct {
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
func (c ConnectCmd) CmdID() dispatcher.CommandID {
	return Connect
}

// SendCmd sends a message on a WebSocket connection.
type SendCmd struct {
	Data        []byte
	ConnID      uint64
	MessageType int
}

// CmdID implements dispatcher.Command.
func (c SendCmd) CmdID() dispatcher.CommandID {
	return Send
}

// ReceiveCmd receives a message from a WebSocket connection.
// Blocks until a message is available or connection closes.
// Returns Message via emit, or nil on close.
type ReceiveCmd struct {
	ConnID uint64
}

// CmdID implements dispatcher.Command.
func (c ReceiveCmd) CmdID() dispatcher.CommandID {
	return Receive
}

// CloseCmd closes a WebSocket connection.
type CloseCmd struct {
	Reason string
	ConnID uint64
	Code   int
}

// CmdID implements dispatcher.Command.
func (c CloseCmd) CmdID() dispatcher.CommandID {
	return Close
}

// PingCmd sends a ping on the connection.
type PingCmd struct {
	Data   []byte
	ConnID uint64
}

// CmdID implements dispatcher.Command.
func (c PingCmd) CmdID() dispatcher.CommandID {
	return Ping
}

// Message represents a received WebSocket message.
type Message struct {
	Data        []byte
	MessageType int  // MessageText or MessageBinary
	EOF         bool // True if connection closed
}

// SubscribeCmd starts a background read loop that sends messages to a relay topic.
// The handler spawns a goroutine that reads until connection closes.
// Each message is delivered to the specified topic via relay.
type SubscribeCmd struct {
	PID    pid.PID
	Topic  string
	ConnID uint64
}

// CmdID implements dispatcher.Command.
func (c SubscribeCmd) CmdID() dispatcher.CommandID {
	return Subscribe
}

// Subscription represents an active subscription to WebSocket messages.
// Stop cancels the per-connection relay read loop the dispatcher spawned for
// this subscription. The process wires it through SetSubscriptionCleanup so
// closeChannel / drain / Abort halt the producer goroutine.
type Subscription struct {
	Stop   func()
	Topic  string
	ConnID uint64
}
