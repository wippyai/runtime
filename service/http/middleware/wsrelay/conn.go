package wsrelay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
	"go.uber.org/zap"
)

// Connection represents a single WebSocket connection and its relay
type Connection struct {
	// Context and cancellation
	ctx       context.Context
	cancelCtx context.CancelFunc

	// WebSocket connection
	conn *websocket.Conn

	// Relay components (immutable after creation)
	wsPID      relay.PID
	host       relay.AttachableHost
	node       relay.Node
	topo       topology.Topology
	transcoder payload.Transcoder

	// Mutable relay state protected by mu
	mu                  sync.RWMutex
	currentTargetPID    relay.PID
	currentMessageTopic relay.Topic
	config              RelayCommand

	// Heartbeat (ticker is thread-safe, interval read after initial setup)
	heartbeatTicker   *time.Ticker
	heartbeatInterval time.Duration

	// Metrics
	connectedAt time.Time
	msgCount    atomic.Int64

	// Channels
	msgCh    chan *relay.Package
	readDone chan struct{}

	// Close handling
	closeReason string

	// Logger
	logger *zap.Logger
}

// NewConnection creates a new WebSocket connection relay
func NewConnection(
	appCtx context.Context,
	wsConn *websocket.Conn,
	targetPID relay.PID,
	config RelayCommand,
	messageTopic relay.Topic,
	serverID registry.ID,
	host relay.AttachableHost,
	node relay.Node,
	topo topology.Topology,
	transcoder payload.Transcoder,
	pidGen *uniqid.PIDGenerator,
	logger *zap.Logger,
) (*Connection, error) {
	// Create context with cancellation
	ctx, cancel := context.WithCancel(appCtx)

	// Validate dependencies
	if host == nil {
		cancel()
		return nil, ErrHostRequired
	}

	if node == nil {
		cancel()
		return nil, ErrNodeRequired
	}

	if transcoder == nil {
		cancel()
		return nil, ErrTranscoderRequired
	}

	// Create a unique PID for this WebSocket connection
	wsPID := pidGen.Generate(serverID.String())

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
		msgCh:               make(chan *relay.Package, 10),
		readDone:            make(chan struct{}),
		logger:              logger.With(zap.String("pid", wsPID.String())),
	}

	// Attach the WebSocket PID to the relay host
	_, err := host.Attach(wsPID, conn.msgCh)
	if err != nil {
		logger.Error("Failed to attach to host", zap.Error(err))
		cancel()
		return nil, NewAttachToRelayError(err)
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
	if err := c.topo.Monitor(c.wsPID, c.currentTargetPID); err != nil {
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

// processMessages handles incoming messages from relay and heartbeat events
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

			c.handleRelayPackage(pkg)
		}
	}
}

// handleWebSocketRead continuously reads messages from WebSocket and forwards them to relay
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
						zap.Int("close_code", int(closeStatus)),
						zap.String("error", err.Error()))
				} else {
					// this is DEBUG level simply because modern browsers and proxies love to close connection without close status
					c.logger.Debug("error reading from WebSocket", zap.Error(err))
				}
				return
			}

			// Increment message counter
			c.msgCount.Add(1)

			// Forward message to relay
			if err := c.forwardMessageToRelay(msgType, data); err != nil {
				c.logger.Error("error forwarding message to relay", zap.Error(err))
				return
			}
		}
	}
}

// forwardMessageToRelay converts WebSocket message to relay message and sends it
func (c *Connection) forwardMessageToRelay(msgType websocket.MessageType, data []byte) error {
	// Convert to relay payload
	var payloadData payload.Payload
	if msgType == websocket.MessageText {
		payloadData = payload.NewString(string(data))
	} else {
		payloadData = payload.NewPayload(data, payload.Bytes)
	}

	// Read target PID and topic under lock
	c.mu.RLock()
	targetPID := c.currentTargetPID
	messageTopic := c.currentMessageTopic
	c.mu.RUnlock()

	// Send to target PID
	msg := relay.NewPackage(c.wsPID, targetPID, messageTopic, payloadData)
	err := c.node.Send(msg)
	if err != nil {
		c.logger.Error("Failed to send message to node", zap.Error(err))
	}
	return err
}

// handleRelayPackage processes a package received from relay
func (c *Connection) handleRelayPackage(pkg *relay.Package) {
	defer relay.ReleasePackage(pkg)

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

// handleExitEvent processes exit events from relay
func (c *Connection) handleExitEvent(payloads []payload.Payload) bool {
	c.mu.RLock()
	targetPID := c.currentTargetPID
	c.mu.RUnlock()

	for _, p := range payloads {
		// Check if the payload is an exit event
		if exitEvent, ok := p.Data().(*topology.ExitEvent); ok {
			// Check if the exit event is for our current target PID
			if exitEvent.From.String() == targetPID.String() {
				c.logger.Debug("target PID exited, closing connection",
					zap.String("target_pid", targetPID.String()),
					zap.Any("result", exitEvent.Result))

				if exitEvent.Result != nil && exitEvent.Result.Error != nil {
					c.logger.Warn("target PID exited with error, closing connection",
						zap.String("target_pid", targetPID.String()),
						zap.Error(exitEvent.Result.Error))
				}

				c.Close("target process exited")
				return true
			}
		}
	}
	return false
}

// handleControlMessage processes control messages from relay
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

	c.mu.Lock()
	// Update message topic if provided
	if command.MessageTopic != "" {
		c.currentMessageTopic = command.MessageTopic
	}

	// Update metadata if provided
	if len(command.Metadata) > 0 {
		c.config.Metadata = command.Metadata
	}
	c.mu.Unlock()

	// Update heartbeat interval if provided (ticker is thread-safe)
	if command.HeartbeatInterval != "" {
		if interval, err := time.ParseDuration(command.HeartbeatInterval); err == nil {
			c.heartbeatInterval = interval
			c.heartbeatTicker.Reset(interval)
		}
	}
}

// handleTargetPIDChange processes a change in target PID
func (c *Connection) handleTargetPIDChange(command RelayCommand) {
	newTarget, err := relay.ParsePID(command.TargetPID)
	if err != nil {
		c.logger.Error("invalid target PID in control command", zap.Error(err))
		return
	}

	c.mu.Lock()
	oldTarget := c.currentTargetPID
	changed := oldTarget.String() != newTarget.String()
	if changed {
		c.currentTargetPID = newTarget
	}
	if len(command.Metadata) > 0 {
		c.config.Metadata = command.Metadata
	}
	c.mu.Unlock()

	if changed {
		// Stop monitoring the old target
		if err := c.topo.Demonitor(c.wsPID, oldTarget); err != nil {
			c.logger.Warn("error releasing monitor for old target", zap.Error(err))
		}

		// Start monitoring the new target
		if err := c.topo.Monitor(c.wsPID, newTarget); err != nil {
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
	}
}

// handleCloseMessage processes a close message from relay
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
func (c *Connection) forwardPayloadToWebSocket(topic relay.Topic, payloads ...payload.Payload) error {
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
		switch p.Format() { //nolint:exhaustive
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
				return ErrExpectedBytesPayload
			}
		case payload.YAML, payload.Golang, payload.GoError:
			fallthrough
		default:
			// Try to transcode to JSON for all other formats
			pj, err := c.transcoder.Transcode(p, payload.JSON)
			if err != nil {
				return NewTranscodeError(err)
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
			return NewMarshalError("message wrapper", err)
		}

		// Write to WebSocket as text message
		if err := c.conn.Write(c.ctx, websocket.MessageText, jsonData); err != nil {
			return NewWebSocketWriteError(err)
		}
	}

	return nil
}

// sendJoinNotification sends a join notification to the target PID
func (c *Connection) sendJoinNotification(targetPID relay.PID) error {
	c.mu.RLock()
	metadata := c.config.Metadata
	c.mu.RUnlock()

	// Create a join info structure that includes both client PID and metadata
	joinInfo := JoinInfo{
		ClientPID: c.wsPID.String(),
		Metadata:  metadata,
	}

	// Marshal the join info to JSON
	joinData, err := json.Marshal(joinInfo)
	if err != nil {
		return NewMarshalJoinInfoError(err)
	}

	// Create and send a single package containing both the client PID and metadata
	joinMsg := relay.NewPackage(
		c.wsPID,
		targetPID,
		WSJoinTopic,
		payload.NewPayload(joinData, payload.JSON),
	)

	return c.node.Send(joinMsg)
}

// sendLeaveNotification sends a leave notification to the target PID
func (c *Connection) sendLeaveNotification(targetPID relay.PID) error {
	c.mu.RLock()
	metadata := c.config.Metadata
	c.mu.RUnlock()

	// Create a leave info structure that includes both client PID and metadata
	leaveInfo := JoinInfo{
		ClientPID: c.wsPID.String(),
		Metadata:  metadata,
	}

	// Marshal the leave info to JSON
	leaveData, err := json.Marshal(leaveInfo)
	if err != nil {
		return NewMarshalLeaveInfoError(err)
	}

	// Create and send a single package containing both the client PID and metadata
	leaveMsg := relay.NewPackage(
		c.wsPID,
		targetPID,
		WSLeaveTopic,
		payload.NewPayload(leaveData, payload.JSON),
	)

	return c.node.Send(leaveMsg)
}

// sendHeartbeat sends a heartbeat message to the current target PID
func (c *Connection) sendHeartbeat() {
	c.mu.RLock()
	targetPID := c.currentTargetPID
	metadata := c.config.Metadata
	c.mu.RUnlock()

	// Create a structured heartbeat info object
	heartbeatInfo := HeartbeatInfo{
		ClientPID:    c.wsPID.String(),
		Uptime:       time.Since(c.connectedAt).String(),
		MessageCount: c.msgCount.Load(),
		Metadata:     metadata,
	}

	// Marshal the heartbeat info to JSON
	heartbeatData, err := json.Marshal(heartbeatInfo)
	if err != nil {
		c.logger.Warn("error marshaling heartbeat info", zap.Error(err))
		return
	}

	// Create and send a single heartbeat message
	heartbeatMsg := relay.NewPackage(
		c.wsPID,
		targetPID,
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

	// Get target PID under lock
	c.mu.RLock()
	targetPID := c.currentTargetPID
	c.mu.RUnlock()

	// Release monitoring of the target PID
	if err := c.topo.Demonitor(c.wsPID, targetPID); err != nil {
		c.logger.Warn("error releasing monitor during cleanup", zap.Error(err))
	}

	// Send leave notification
	if err := c.sendLeaveNotification(targetPID); err != nil {
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
