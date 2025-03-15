package websocket_relay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/uniqid"
	"go.uber.org/zap"
)

// Connection represents a single WebSocket connection and its pubsub relay
type Connection struct {
	// Context and cancellation
	ctx       context.Context
	cancelCtx context.CancelFunc

	// WebSocket connection
	conn *websocket.Conn

	// PubSub components
	wsPID               pubsub.PID
	currentTargetPID    pubsub.PID
	currentMessageTopic pubsub.Topic
	host                pubsub.Host
	node                pubsub.Node
	transcoder          payload.Transcoder

	// Configuration
	config            RelayCommand
	heartbeatTicker   *time.Ticker
	heartbeatInterval time.Duration

	// Metrics
	connectedAt time.Time
	msgCount    atomic.Int64

	// Channels
	msgCh    chan *pubsub.Package
	readDone chan struct{}

	// Logger
	logger *zap.Logger
}

// NewConnection creates a new WebSocket connection relay
func NewConnection(
	appCtx context.Context,
	wsConn *websocket.Conn,
	targetPID pubsub.PID,
	config RelayCommand,
	messageTopic pubsub.Topic,
	serverID registry.ID,
	host pubsub.Host,
	node pubsub.Node,
	transcoder payload.Transcoder,
	idGen *uniqid.Generator, // Add idGen parameter
	logger *zap.Logger,
) (*Connection, error) {
	// Create context with cancellation
	ctx, cancel := context.WithCancel(appCtx)

	// Validate dependencies
	if host == nil {
		cancel()
		return nil, fmt.Errorf("host is required")
	}

	if node == nil {
		cancel()
		return nil, fmt.Errorf("node is required")
	}

	if transcoder == nil {
		cancel()
		return nil, fmt.Errorf("transcoder is required")
	}

	// Create a unique PID for this WebSocket connection
	wsPID := pubsub.PID{
		Node:   node.ID(),
		Host:   serverID.String(),
		ID:     registry.ParseID("ws:conn"),
		UniqID: idGen.Generate(), // Use the passed idGen
	}

	// Parse heartbeat interval
	heartbeatInterval := DefaultHeartbeatInterval
	if config.HeartbeatInterval != "" {
		if interval, err := time.ParseDuration(config.HeartbeatInterval); err == nil {
			heartbeatInterval = interval
		}
	}

	// Create the connection instance
	conn := &Connection{
		ctx:                 ctx,
		cancelCtx:           cancel,
		conn:                wsConn,
		wsPID:               wsPID,
		currentTargetPID:    targetPID,
		currentMessageTopic: messageTopic,
		host:                host,
		node:                node,
		transcoder:          transcoder,
		config:              config,
		heartbeatInterval:   heartbeatInterval,
		heartbeatTicker:     time.NewTicker(heartbeatInterval),
		connectedAt:         time.Now(),
		msgCh:               make(chan *pubsub.Package, 10),
		readDone:            make(chan struct{}),
		logger:              logger.With(zap.String("pid", wsPID.String())),
	}

	// Attach the WebSocket PID to the relay host
	cancelAttach, err := host.Attach(wsPID, conn.msgCh)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to attach to relay: %w", err)
	}

	// Store the cancel function for later use
	originalCancel := conn.cancelCtx
	conn.cancelCtx = func() {
		cancelAttach()
		originalCancel()
	}

	return conn, nil
}

// Serve begins processing WebSocket communication
func (c *Connection) Serve() {
	// Send initial join notification with metadata
	if err := c.sendJoinNotification(c.currentTargetPID); err != nil {
		c.logger.Error("Failed to send join notification", zap.Error(err))
		c.Close("Failed to send join notification")
		return
	}

	// Serve a goroutine to handle reading from WebSocket (client -> pubsub)
	go c.handleWebSocketRead()

	// Main loop to process messages
	c.processMessages()
}

// processMessages handles incoming messages from pubsub and heartbeat events
func (c *Connection) processMessages() {
	defer c.cleanup()

	for {
		select {
		case <-c.ctx.Done():
			return

		case <-c.readDone:
			return

		case <-c.heartbeatTicker.C:
			c.sendHeartbeat()

		case pkg, ok := <-c.msgCh:
			if !ok {
				c.logger.Debug("Message channel closed")
				return
			}

			c.handlePubSubPackage(pkg)
		}
	}
}

// handleWebSocketRead continuously reads messages from WebSocket and forwards them to pubsub
func (c *Connection) handleWebSocketRead() {
	defer close(c.readDone)
	defer c.cancelCtx()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			// Read from WebSocket
			msgType, data, err := c.conn.Read(c.ctx)
			if err != nil {
				if closeStatus := websocket.CloseStatus(err); closeStatus != -1 {
					c.logger.Info("WebSocket closed by client",
						zap.Int("closeCode", int(closeStatus)),
						zap.String("error", err.Error()))
				} else {
					c.logger.Error("Error reading from WebSocket", zap.Error(err))
				}
				return
			}

			// Increment message counter
			c.msgCount.Add(1)

			// Forward message to pubsub
			if err := c.forwardMessageToPubSub(msgType, data); err != nil {
				c.logger.Error("Error forwarding message to pubsub", zap.Error(err))
				return
			}
		}
	}
}

// forwardMessageToPubSub converts WebSocket message to pubsub message and sends it
func (c *Connection) forwardMessageToPubSub(msgType websocket.MessageType, data []byte) error {
	// Convert to pubsub payload
	var payloadData payload.Payload
	if msgType == websocket.MessageText {
		payloadData = payload.NewString(string(data))
	} else {
		payloadData = payload.NewPayload(data, payload.Bytes)
	}

	// Send to target PID
	msg := pubsub.NewPackage(c.wsPID, c.currentTargetPID, c.currentMessageTopic, payloadData)
	return c.node.Send(msg)
}

// handlePubSubPackage processes a package received from pubsub
func (c *Connection) handlePubSubPackage(pkg *pubsub.Package) {
	for _, msg := range pkg.Messages {
		// Handle control messages
		if msg.Topic == WSControlTopic && len(msg.Payloads) > 0 {
			c.handleControlMessage(msg.Payloads[0])
			continue
		}

		// Handle close messages
		if msg.Topic == WSCloseTopic {
			c.handleCloseMessage(msg.Payloads)
			return
		}

		// Forward regular messages to WebSocket
		if len(msg.Payloads) > 0 {
			if err := c.forwardPayloadToWebSocket(msg.Topic, msg.Payloads...); err != nil {
				c.logger.Error("Error forwarding payload to WebSocket", zap.Error(err))
				c.cancelCtx()
				return
			}
		}
	}
}

// handleControlMessage processes control messages from pubsub
func (c *Connection) handleControlMessage(p payload.Payload) {
	var command RelayCommand
	if err := c.transcoder.Unmarshal(p, &command); err != nil {
		c.logger.Error("Failed to unmarshal control payload", zap.Error(err))
		return
	}

	// Update target PID if provided
	if command.TargetPID != "" {
		c.handleTargetPIDChange(command)
	}

	// Update message topic if provided
	if command.MessageTopic != "" {
		c.currentMessageTopic = pubsub.Topic(command.MessageTopic)
		c.logger.Info("Updated message topic", zap.String("newTopic", command.MessageTopic))
	}

	// Update heartbeat interval if provided
	if command.HeartbeatInterval != "" {
		if interval, err := time.ParseDuration(command.HeartbeatInterval); err == nil {
			c.heartbeatInterval = interval
			c.heartbeatTicker.Reset(interval)
			c.logger.Info("Updated heartbeat interval", zap.Duration("newInterval", interval))
		}
	}

	// Update metadata if provided
	if command.Metadata != nil && len(command.Metadata) > 0 {
		c.config.Metadata = command.Metadata
		c.logger.Info("Updated metadata", zap.Any("metadata", command.Metadata))
	}
}

// handleTargetPIDChange processes a change in target PID
func (c *Connection) handleTargetPIDChange(command RelayCommand) {
	oldTarget := c.currentTargetPID
	newTarget, err := pubsub.ParsePID(command.TargetPID)
	if err != nil {
		c.logger.Error("Invalid target PID in control command", zap.Error(err))
		return
	}

	if oldTarget.String() != newTarget.String() {
		// Send leave to old target
		if err := c.sendLeaveNotification(oldTarget); err != nil {
			c.logger.Error("Error sending leave on target change", zap.Error(err))
		}

		// Send join to new target
		if err := c.sendJoinNotification(newTarget); err != nil {
			c.logger.Error("Error sending join on target change", zap.Error(err))
		}

		// Store any updated metadata
		if command.Metadata != nil && len(command.Metadata) > 0 {
			c.config.Metadata = command.Metadata
			c.logger.Info("Updated metadata on target change", zap.Any("metadata", command.Metadata))
		}
	}

	c.currentTargetPID = newTarget
	c.logger.Info("Updated target PID", zap.String("newTarget", newTarget.String()))
}

// handleCloseMessage processes a close message from pubsub
func (c *Connection) handleCloseMessage(payloads []payload.Payload) {
	reason := "Connection closed by server"

	if len(payloads) > 0 {
		// Try to extract reason and code from payload
		payloadData := payloads[0].Data()
		if str, ok := payloadData.(string); ok {
			reason = str
		} else if data, err := json.Marshal(payloadData); err == nil {
			reason = string(data)
		}
	}

	c.logger.Info("Received close command", zap.String("reason", reason))
	c.Close(reason)
}

// forwardPayloadToWebSocket sends payloads to the WebSocket client
// with topic information and support for multiple payloads
func (c *Connection) forwardPayloadToWebSocket(topic pubsub.Topic, payloads ...payload.Payload) error {
	if len(payloads) == 0 {
		return nil
	}

	// Process each payload as a separate message
	for _, p := range payloads {
		// Create a wrapper object that includes the topic and data
		wrapper := struct {
			Topic string      `json:"topic"`
			Data  interface{} `json:"data"`
		}{
			Topic: string(topic),
		}

		// Process different payload types
		switch p.Format() {
		case payload.String:
			// For string payloads, use the string data directly
			if str, ok := p.Data().(string); ok {
				wrapper.Data = str
			} else {
				wrapper.Data = fmt.Sprintf("%v", p.Data())
			}

		case payload.JSON:
			// For JSON payloads, use the data as-is without double parsing
			if bytes, ok := p.Data().([]byte); ok {
				// Raw JSON bytes - pass as raw message
				// This avoids unnecessary unmarshaling and remarshaling
				rawMsg := json.RawMessage(bytes)
				wrapper.Data = rawMsg
			} else if str, ok := p.Data().(string); ok {
				// JSON string - convert to raw message
				rawMsg := json.RawMessage(str)
				wrapper.Data = rawMsg
			} else {
				// Already a structured object, use directly
				wrapper.Data = p.Data()
			}

		case payload.Bytes:
			// For binary data, encode as base64 string for JSON compatibility
			if bytes, ok := p.Data().([]byte); ok {
				wrapper.Data = base64.StdEncoding.EncodeToString(bytes)
			} else {
				return fmt.Errorf("expected bytes payload but got different type")
			}

		default:
			// Try to transcode to JSON for all other formats
			pj, err := c.transcoder.Transcode(p, payload.JSON)
			if err != nil {
				return fmt.Errorf("failed to transcode payload to JSON: %w", err)
			}

			// Use the transcoded JSON data directly as a RawMessage
			if jsonBytes, ok := pj.Data().([]byte); ok {
				wrapper.Data = json.RawMessage(jsonBytes)
			} else {
				wrapper.Data = pj.Data()
			}
		}

		// Marshal the wrapper to JSON and send it
		jsonData, err := json.Marshal(wrapper)
		if err != nil {
			return fmt.Errorf("error marshaling message wrapper: %w", err)
		}

		// Write to WebSocket as text message
		if err := c.conn.Write(c.ctx, websocket.MessageText, jsonData); err != nil {
			return fmt.Errorf("error writing to WebSocket: %w", err)
		}
	}

	return nil
}

// writeStringPayload writes a string payload to WebSocket
func (c *Connection) writeStringPayload(p payload.Payload, msgType websocket.MessageType) error {
	if str, ok := p.Data().(string); ok {
		return c.conn.Write(c.ctx, msgType, []byte(str))
	}

	// Convert to string if not already
	strData := fmt.Sprintf("%v", p.Data())
	return c.conn.Write(c.ctx, msgType, []byte(strData))
}

// writeJSONPayload writes a JSON payload to WebSocket
func (c *Connection) writeJSONPayload(p payload.Payload, msgType websocket.MessageType) error {
	var data []byte
	var err error

	// If already JSON bytes, use directly
	if bytes, ok := p.Data().([]byte); ok {
		data = bytes
	} else if str, ok := p.Data().(string); ok {
		// If it's a JSON string, use directly
		data = []byte(str)
	} else {
		// Otherwise marshal the data to JSON
		data, err = json.Marshal(p.Data())
		if err != nil {
			return fmt.Errorf("error marshaling JSON payload: %w", err)
		}
	}

	// Send the JSON directly to WebSocket
	return c.conn.Write(c.ctx, msgType, data)
}

// writeBinaryPayload writes a binary payload to WebSocket
func (c *Connection) writeBinaryPayload(p payload.Payload) error {
	if bytes, ok := p.Data().([]byte); ok {
		return c.conn.Write(c.ctx, websocket.MessageBinary, bytes)
	}

	return fmt.Errorf("expected bytes payload but got different type")
}

// writeTranscodedPayload transcodes a payload to JSON and writes it to WebSocket
func (c *Connection) writeTranscodedPayload(p payload.Payload, msgType websocket.MessageType) error {
	pj, err := c.transcoder.Transcode(p, payload.JSON)
	if err != nil {
		return fmt.Errorf("failed to transcode payload to JSON: %w", err)
	}

	return c.conn.Write(c.ctx, msgType, pj.Data().([]byte))
}

// sendJoinNotification sends a join notification to the target PID
func (c *Connection) sendJoinNotification(targetPID pubsub.PID) error {
	// The Lua hub expects a simple string PID format, so we'll use that directly
	// while storing metadata in a separate message if needed
	joinMsg := pubsub.NewPackage(c.wsPID, targetPID, WSJoinTopic, payload.NewString(c.wsPID.String()))

	// If metadata exists, send it in a separate metadata message
	if c.config.Metadata != nil && len(c.config.Metadata) > 0 {
		metadataInfo := map[string]interface{}{
			"type":       "client_metadata",
			"client_pid": c.wsPID.String(),
			"metadata":   c.config.Metadata,
		}

		metadataData, err := json.Marshal(metadataInfo)
		if err != nil {
			c.logger.Warn("Error marshaling metadata", zap.Error(err))
		} else {
			metadataMsg := pubsub.NewPackage(c.wsPID, targetPID, WSMessageTopic,
				payload.NewPayload(metadataData, payload.JSON))

			if err := c.node.Send(metadataMsg); err != nil {
				c.logger.Warn("Error sending metadata message", zap.Error(err))
			}
		}
	}

	return c.node.Send(joinMsg)
}

// sendLeaveNotification sends a leave notification to the target PID
func (c *Connection) sendLeaveNotification(targetPID pubsub.PID) error {
	// Use simple string format as expected by Lua hub
	leaveMsg := pubsub.NewPackage(c.wsPID, targetPID, WSLeaveTopic, payload.NewString(c.wsPID.String()))
	return c.node.Send(leaveMsg)
}

// sendHeartbeat sends a heartbeat message to the current target PID
func (c *Connection) sendHeartbeat() {
	// Format compatible with existing Lua code expectations
	// Create a simple comma-separated string with the information the Lua code needs
	heartbeatString := fmt.Sprintf("%s,%s,%d",
		c.wsPID.String(),
		time.Since(c.connectedAt).String(),
		c.msgCount.Load())

	heartbeatMsg := pubsub.NewPackage(c.wsPID, c.currentTargetPID, WSHeartbeatTopic,
		payload.NewString(heartbeatString))

	// Send metadata in a separate message if available
	if c.config.Metadata != nil && len(c.config.Metadata) > 0 {
		metadataInfo := map[string]interface{}{
			"type":       "heartbeat_metadata",
			"client_pid": c.wsPID.String(),
			"uptime":     time.Since(c.connectedAt).String(),
			"count":      c.msgCount.Load(),
			"metadata":   c.config.Metadata,
		}

		metadataData, err := json.Marshal(metadataInfo)
		if err != nil {
			c.logger.Warn("Error marshaling heartbeat metadata", zap.Error(err))
		} else {
			metadataMsg := pubsub.NewPackage(c.wsPID, c.currentTargetPID, WSMessageTopic,
				payload.NewPayload(metadataData, payload.JSON))

			if err := c.node.Send(metadataMsg); err != nil {
				c.logger.Warn("Error sending heartbeat metadata message", zap.Error(err))
			}
		}
	}

	if err := c.node.Send(heartbeatMsg); err != nil {
		c.logger.Warn("Error sending heartbeat", zap.Error(err))
	}
}

// Close closes the WebSocket connection with the specified reason
func (c *Connection) Close(reason string) {
	if err := c.conn.Close(websocket.StatusNormalClosure, reason); err != nil {
		c.logger.Error("Error closing WebSocket connection", zap.Error(err))
	}
	c.cancelCtx()
}

// cleanup performs cleanup operations when the connection is closed
func (c *Connection) cleanup() {
	// Stop the heartbeat ticker
	c.heartbeatTicker.Stop()

	// Send leave notification
	if err := c.sendLeaveNotification(c.currentTargetPID); err != nil {
		c.logger.Error("Error sending leave message", zap.Error(err))
	}

	// Detach from host
	c.host.Detach(c.wsPID)

	c.logger.Info("WebSocket connection closed")
}
