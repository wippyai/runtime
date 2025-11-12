package websocketrelay

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
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/topology"
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
	topo                topology.Topology
	transcoder          payload.Transcoder

	// Configuration
	config            RelayCommand
	heartbeatTicker   *time.Ticker
	heartbeatInterval time.Duration
	closeReason       string

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
	topo topology.Topology,
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
		UniqID: idGen.Generate(), // Use the passed idGen
	}.Precomputed()

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
		topo:                topo,
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
	_, err := host.Attach(wsPID, conn.msgCh)
	if err != nil {
		logger.Error("Failed to attach to host", zap.Error(err))
		cancel()
		return nil, fmt.Errorf("failed to attach to relay: %w", err)
	}

	return conn, nil
}

// Serve begins processing WebSocket communication
func (c *Connection) Serve() {
	err := c.topo.Register(c.wsPID)
	if err != nil {
		c.logger.Error("failed to register WebSocket PID", zap.Error(err))
		c.Close("Failed to register WebSocket PID")
		return
	}

	// Start monitoring the target PID
	if err := c.topo.Wait(c.wsPID, c.currentTargetPID); err != nil {
		c.logger.Error("failed to monitor target PID", zap.Error(err))
		c.Close("Failed to monitor target PID")
		return
	}

	// Send initial join notification with metadata
	if err := c.sendJoinNotification(c.currentTargetPID); err != nil {
		c.logger.Error("failed to send join notification", zap.Error(err))
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
			c.logger.Info("Read done, exiting message loop")
			return

		case <-c.heartbeatTicker.C:
			c.sendHeartbeat()

		case pkg, ok := <-c.msgCh:
			if !ok {
				c.logger.Info("Message channel closed, exiting message loop")
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
					c.logger.Debug("websocket closed by client",
						zap.Int("closeCode", int(closeStatus)),
						zap.String("error", err.Error()))
				} else {
					// this is DEBUG level simply because modern browsers and proxies love to close connection without close status
					c.logger.Debug("error reading from WebSocket", zap.Error(err))
				}
				return
			}

			// Increment message counter
			c.msgCount.Add(1)

			// Forward message to pubsub
			if err := c.forwardMessageToPubSub(msgType, data); err != nil {
				c.logger.Error("error forwarding message to pubsub", zap.Error(err))
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

	c.logger.Info("Forwarding message from WebSocket to pubsub",
		zap.String("from", c.wsPID.String()),
		zap.String("to", c.currentTargetPID.String()),
		zap.String("topic", string(c.currentMessageTopic)),
		zap.Int("data_len", len(data)))

	// Send to target PID
	msg := pubsub.NewPackage(c.wsPID, c.currentTargetPID, c.currentMessageTopic, payloadData)
	err := c.node.Send(msg)
	if err != nil {
		c.logger.Error("Failed to send message to node", zap.Error(err))
	}
	return err
}

// handlePubSubPackage processes a package received from pubsub
func (c *Connection) handlePubSubPackage(pkg *pubsub.Package) {
	defer pubsub.ReleasePackage(pkg)

	for _, msg := range pkg.Messages {
		// Handle exit events for the target PID
		if msg.Topic == topology.TopicEvents && len(msg.Payloads) > 0 {
			if c.handleExitEvent(msg.Payloads) {
				// Exit event was for current target PID, we're closing
				return
			}
		}

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
				c.logger.Error("error forwarding payload to WebSocket", zap.Error(err))
				c.cancelCtx()
				return
			}
		}
	}
}

// handleExitEvent processes exit events from pubsub
func (c *Connection) handleExitEvent(payloads []payload.Payload) bool {
	for _, p := range payloads {
		// Check if the payload is an exit event
		if exitEvent, ok := p.Data().(*topology.ExitEvent); ok {
			// Check if the exit event is for our current target PID
			if exitEvent.From.String() == c.currentTargetPID.String() {
				c.logger.Debug("target PID exited, closing connection",
					zap.String("targetPID", c.currentTargetPID.String()),
					zap.Any("result", exitEvent.Result))

				if exitEvent.Result != nil && exitEvent.Result.Error != nil {
					c.logger.Warn("target PID exited with error, closing connection",
						zap.String("targetPID", c.currentTargetPID.String()),
						zap.Error(exitEvent.Result.Error))
				}

				c.Close("target process exited")
				return true
			}
		}
	}
	return false
}

// handleControlMessage processes control messages from pubsub
func (c *Connection) handleControlMessage(p payload.Payload) {
	var command RelayCommand
	if err := c.transcoder.Unmarshal(p, &command); err != nil {
		c.logger.Error("failed to unmarshal control payload", zap.Error(err))
		return
	}

	// Update target PID if provided
	if command.TargetPID != "" {
		c.handleTargetPIDChange(command)
	}

	// Update message topic if provided
	if command.MessageTopic != "" {
		c.currentMessageTopic = command.MessageTopic
		c.logger.Debug("updated message topic", zap.String("newTopic", command.MessageTopic))
	}

	// Update heartbeat interval if provided
	if command.HeartbeatInterval != "" {
		if interval, err := time.ParseDuration(command.HeartbeatInterval); err == nil {
			c.heartbeatInterval = interval
			c.heartbeatTicker.Reset(interval)
			c.logger.Debug("updated heartbeat interval", zap.Duration("newInterval", interval))
		}
	}

	// Update metadata if provided
	if len(command.Metadata) > 0 {
		c.config.Metadata = command.Metadata
		c.logger.Debug("updated metadata", zap.Any("metadata", command.Metadata))
	}
}

// handleTargetPIDChange processes a change in target PID
func (c *Connection) handleTargetPIDChange(command RelayCommand) {
	oldTarget := c.currentTargetPID
	newTarget, err := pubsub.ParsePID(command.TargetPID)
	if err != nil {
		c.logger.Error("invalid target PID in control command", zap.Error(err))
		return
	}

	if oldTarget.String() != newTarget.String() {
		// Stop monitoring the old target
		if err := c.topo.Release(c.wsPID, oldTarget); err != nil {
			c.logger.Warn("error releasing monitor for old target", zap.Error(err))
		}

		// Start monitoring the new target
		if err := c.topo.Wait(c.wsPID, newTarget); err != nil {
			c.logger.Error("failed to monitor new target PID", zap.Error(err))
			c.Close("Failed to monitor new target PID")
			return
		}

		// Send leave to old target
		if err := c.sendLeaveNotification(oldTarget); err != nil {
			c.logger.Error("error sending leave on target change", zap.Error(err))
		}

		// Send join to new target
		if err := c.sendJoinNotification(newTarget); err != nil {
			c.logger.Error("error sending join on target change", zap.Error(err))
		}

		// Store any updated metadata
		if len(command.Metadata) > 0 {
			c.config.Metadata = command.Metadata
			c.logger.Debug("updated metadata on target change", zap.Any("metadata", command.Metadata))
		}
	}

	c.currentTargetPID = newTarget
	c.logger.Debug("updated target PID", zap.String("newTarget", newTarget.String()))
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

	c.logger.Debug("received close command", zap.String("reason", reason))
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
			Topic string `json:"topic"`
			Data  any    `json:"data"`
		}{
			Topic: topic,
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
		case payload.YAML, payload.Golang, payload.Lua, payload.Error:
			// FIXME rework on demand
			fallthrough
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

// sendJoinNotification sends a join notification to the target PID
func (c *Connection) sendJoinNotification(targetPID pubsub.PID) error {
	// Create a join info structure that includes both client PID and metadata
	joinInfo := JoinInfo{
		ClientPID: c.wsPID.String(),
		Metadata:  c.config.Metadata,
	}

	// Marshal the join info to JSON
	joinData, err := json.Marshal(joinInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal join info: %w", err)
	}

	// Create and send a single package containing both the client PID and metadata
	joinMsg := pubsub.NewPackage(
		c.wsPID,
		targetPID,
		WSJoinTopic,
		payload.NewPayload(joinData, payload.JSON),
	)

	return c.node.Send(joinMsg)
}

// sendLeaveNotification sends a leave notification to the target PID
func (c *Connection) sendLeaveNotification(targetPID pubsub.PID) error {
	// Create a join info structure that includes both client PID and metadata
	leaveInfo := JoinInfo{
		ClientPID: c.wsPID.String(),
		Metadata:  c.config.Metadata,
	}

	// Marshal the leave info to JSON
	leaveData, err := json.Marshal(leaveInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal leave info: %w", err)
	}

	// Create and send a single package containing both the client PID and metadata
	leaveMsg := pubsub.NewPackage(
		c.wsPID,
		targetPID,
		WSLeaveTopic,
		payload.NewPayload(leaveData, payload.JSON),
	)

	return c.node.Send(leaveMsg)
}

// sendHeartbeat sends a heartbeat message to the current target PID
func (c *Connection) sendHeartbeat() {
	// Create a structured heartbeat info object
	heartbeatInfo := HeartbeatInfo{
		ClientPID:    c.wsPID.String(),
		Uptime:       time.Since(c.connectedAt).String(),
		MessageCount: c.msgCount.Load(),
		Metadata:     c.config.Metadata,
	}

	// Marshal the heartbeat info to JSON
	heartbeatData, err := json.Marshal(heartbeatInfo)
	if err != nil {
		c.logger.Warn("error marshaling heartbeat info", zap.Error(err))
		return
	}

	// Create and send a single heartbeat message
	heartbeatMsg := pubsub.NewPackage(
		c.wsPID,
		c.currentTargetPID,
		WSHeartbeatTopic,
		payload.NewPayload(heartbeatData, payload.JSON),
	)

	if err := c.node.Send(heartbeatMsg); err != nil {
		c.logger.Warn("error sending heartbeat", zap.Error(err))
	}
}

// Close closes the WebSocket connection with the specified reason
func (c *Connection) Close(reason string) {
	// Store the reason for later use in the notify
	c.closeReason = reason

	if err := c.conn.Close(websocket.StatusNormalClosure, reason); err != nil {
		c.logger.Error("error closing WebSocket connection", zap.Error(err))
	}
	c.cancelCtx()
}

// cleanup performs cleanup operations when the connection is closed
func (c *Connection) cleanup() {
	// Stop the heartbeat ticker
	c.heartbeatTicker.Stop()

	// Release monitoring of the target PID
	if err := c.topo.Release(c.wsPID, c.currentTargetPID); err != nil {
		c.logger.Warn("error releasing monitor during cleanup", zap.Error(err))
	}

	// Send leave notification
	if err := c.sendLeaveNotification(c.currentTargetPID); err != nil {
		c.logger.Error("error sending leave message", zap.Error(err))
	}

	// Notify topology about our exit
	result := &runtime.Result{
		Value: payload.NewString("websocket connection closed"),
		Error: nil,
	}

	c.topo.Notify(c.wsPID, result)
	c.topo.Remove(c.wsPID)

	// Detach from host
	c.host.Detach(c.wsPID)

	c.logger.Debug("websocket connection closed")
}
