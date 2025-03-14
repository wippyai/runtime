package websocket_relay

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	httpapi "github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/internal/uniqid"
	"go.uber.org/zap"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Constants for the WebSocket relay
const (
	// WSRelayHeader is the header that indicates a WebSocket connection request
	WSRelayHeader = "X-WS-Relay"

	// Topic constants
	WSMessageTopic   pubsub.Topic = "ws.message"
	WSJoinTopic      pubsub.Topic = "ws.join"
	WSLeaveTopic     pubsub.Topic = "ws.leave"
	WSControlTopic   pubsub.Topic = "ws.control"
	WSCloseTopic     pubsub.Topic = "ws.close"
	WSHeartbeatTopic pubsub.Topic = "ws.heartbeat"

	// Default heartbeat interval
	DefaultHeartbeatInterval = 30 * time.Second
)

// RelayCommand holds the configuration for a WebSocket relay request
type RelayCommand struct {
	TargetPID         string `json:"target_pid"`
	MessageTopic      string `json:"message_topic,omitempty"`
	HeartbeatInterval string `json:"heartbeat_interval,omitempty"`
}

// RelayManager manages WebSocket connections and their relay to the pubsub system
type RelayManager struct {
	appCtx context.Context
	logger *zap.Logger
	idGen  *uniqid.Generator
}

// NewWebSocketRelay creates a new WebSocket relay manager
func NewWebSocketRelay(ctx context.Context, logger *zap.Logger) *RelayManager {
	return &RelayManager{
		appCtx: ctx,
		logger: logger,
		idGen:  uniqid.NewGenerator(),
	}
}

// Middleware creates an HTTP middleware function that handles WebSocket relay requests
func (m *RelayManager) Middleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrappedWriter := newResponseWrapper(w)

		// Call the next handler
		h.ServeHTTP(wrappedWriter, r)

		// Check if the handler added our special header to the response
		relayConfigStr := wrappedWriter.Header().Get(WSRelayHeader)
		if relayConfigStr == "" {
			return // not our business
		}
		w.Header().Del(WSRelayHeader)

		// Get context logger and add request info
		logger := m.logger.With(
			zap.String("path", r.URL.Path),
			zap.String("remoteAddr", r.RemoteAddr),
		)

		logger.Debug("Handling WebSocket relay request", zap.String("config", relayConfigStr))

		// Parse the relay configuration
		var config RelayCommand
		if err := json.Unmarshal([]byte(relayConfigStr), &config); err != nil {
			logger.Error("Invalid relay configuration", zap.Error(err))
			return
		}

		// Parse the target PID
		targetPID, err := pubsub.ParsePID(config.TargetPID)
		if err != nil {
			logger.Error("Invalid target PID", zap.Error(err), zap.String("pid", config.TargetPID))
			http.Error(w, "Invalid target PID: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Set default message topic if not provided
		messageTopic := WSMessageTopic
		if config.MessageTopic != "" {
			messageTopic = pubsub.Topic(config.MessageTopic)
		}

		// Upgrade the connection to WebSocket
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			logger.Error("Error upgrading to WebSocket", zap.Error(err))
			return
		}

		// Handle the WebSocket connection with the request context
		go m.handleConnection(r.Context(), conn, targetPID, config, messageTopic)
	})
}

// handleConnection manages a WebSocket connection and its bidirectional communication
func (m *RelayManager) handleConnection(
	ctx context.Context,
	conn *websocket.Conn,
	targetPID pubsub.PID,
	config RelayCommand,
	messageTopic pubsub.Topic,
) {
	// Create logger with connection info
	connLogger := m.logger.With(
		zap.String("targetPID", targetPID.String()),
		zap.String("topic", string(messageTopic)),
	)

	// Get host from context
	host := pubsub.GetHost(ctx)
	if host == nil {
		connLogger.Error("Host not found in context")
		conn.Close(websocket.StatusInternalError, "Host not found in context")
		return
	}

	// Get node from context
	node := pubsub.GetNode(ctx)
	if node == nil {
		connLogger.Error("Node not found in context")
		conn.Close(websocket.StatusInternalError, "Node not found in context")
		return
	}

	// Get server ID from context
	serverID, ok := ctx.Value(httpapi.ContextServerID).(registry.ID)
	if !ok || serverID.String() == "" {
		connLogger.Error("Server ID not found in context")
		conn.Close(websocket.StatusInternalError, "Server ID not found in context")
		return
	}

	// Get transcoder from context
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		connLogger.Error("Transcoder not found in context")
		conn.Close(websocket.StatusInternalError, "Transcoder not found in context")
		return
	}

	// Create a unique PID for this WebSocket connection
	wsPID := pubsub.PID{
		Node:   node.ID(),
		Host:   serverID.String(),
		ID:     registry.ParseID("ws:conn"),
		UniqID: m.idGen.Generate(),
	}

	// Update logger with WebSocket ID
	connLogger = connLogger.With(zap.String("pid", wsPID.String()))
	connLogger.Info("WebSocket connection established")

	// Connection metrics
	connectedAt := time.Now()
	msgCount := atomic.Int64{}

	// Create a channel for receiving messages from pubsub
	msgCh := make(chan *pubsub.Package, 10)

	// Attach the WebSocket PID to the relay host
	cancel, err := host.Attach(wsPID, msgCh)
	if err != nil {
		connLogger.Error("Error attaching WebSocket to relay host", zap.Error(err))
		conn.Close(websocket.StatusInternalError, "Failed to attach to relay")
		return
	}
	defer cancel()

	// Create a context with cancellation for the WebSocket handlers
	wsCtx, wsCancel := context.WithCancel(m.appCtx)
	defer wsCancel()

	// Atomic variables for configuration changes
	var currentTargetPID atomic.Value
	var currentMessageTopic atomic.Value

	currentTargetPID.Store(targetPID)
	currentMessageTopic.Store(messageTopic)

	// Parse heartbeat interval
	heartbeatInterval := DefaultHeartbeatInterval
	if config.HeartbeatInterval != "" {
		if interval, err := time.ParseDuration(config.HeartbeatInterval); err == nil {
			heartbeatInterval = interval
		}
	}

	// Create a heartbeat ticker
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer heartbeatTicker.Stop()

	// Send a join notification - use string to keep it simple and reliable
	joinInfo := fmt.Sprintf("%s", wsPID.String())
	joinMsg := pubsub.NewPackage(wsPID, targetPID, WSJoinTopic, payload.NewString(joinInfo))
	if err := node.Send(joinMsg); err != nil {
		connLogger.Error("Error sending join message", zap.Error(err))
		conn.Close(websocket.StatusInternalError, "Error sending join message")
		return
	}

	// Create a WaitGroup for the goroutines
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Handle incoming WebSocket messages (client -> pubsub)
	go func() {
		defer wg.Done()
		defer wsCancel() // Ensure context is cancelled when this goroutine exits

		for {
			select {
			case <-wsCtx.Done():
				return
			default:
				// Read from WebSocket
				msgType, data, err := conn.Read(wsCtx)
				if err != nil {
					if closeStatus := websocket.CloseStatus(err); closeStatus != -1 {
						connLogger.Info("WebSocket closed by client",
							zap.Int("closeCode", int(closeStatus)),
							zap.String("error", err.Error()))
					} else {
						connLogger.Error("Error reading from WebSocket", zap.Error(err))
					}
					return
				}

				// Increment message counter
				msgCount.Add(1)

				// Convert to pubsub payload
				var payloadData payload.Payload
				if msgType == websocket.MessageText {
					payloadData = payload.NewString(string(data))
				} else {
					payloadData = payload.NewPayload(data, payload.Bytes)
				}

				// Get current config values
				currentTarget := currentTargetPID.Load().(pubsub.PID)
				currentTopic := currentMessageTopic.Load().(pubsub.Topic)

				// Send to target PID
				msg := pubsub.NewPackage(wsPID, currentTarget, currentTopic, payloadData)
				if err := node.Send(msg); err != nil {
					connLogger.Error("Error sending to pubsub", zap.Error(err))
					return
				}
			}
		}
	}()

	// Goroutine 2: Handle pubsub messages and internal events (pubsub -> client)
	go func() {
		defer wg.Done()
		defer wsCancel() // Ensure context is cancelled when this goroutine exits

		for {
			select {
			case <-wsCtx.Done():
				return

			case <-heartbeatTicker.C:
				// Send heartbeat to target
				currentTarget := currentTargetPID.Load().(pubsub.PID)
				uptime := time.Since(connectedAt).String()
				msgCountVal := msgCount.Load()

				heartbeatInfo := fmt.Sprintf("%s,%s,%d", wsPID.String(), uptime, msgCountVal)
				heartbeatMsg := pubsub.NewPackage(wsPID, currentTarget, WSHeartbeatTopic, payload.NewString(heartbeatInfo))

				if err := node.Send(heartbeatMsg); err != nil {
					connLogger.Warn("Error sending heartbeat", zap.Error(err))
				}

			case pkg, ok := <-msgCh:
				if !ok {
					connLogger.Debug("Message channel closed")
					return
				}

				for _, msg := range pkg.Messages {
					// Handle control messages
					if msg.Topic == WSControlTopic && len(msg.Payloads) > 0 {
						var command RelayCommand
						if err := dtt.Unmarshal(msg.Payloads[0], &command); err != nil {
							connLogger.Error("Failed to unmarshal control payload", zap.Error(err))
							continue
						}

						// Update target PID if provided
						if command.TargetPID != "" {
							oldTarget := currentTargetPID.Load().(pubsub.PID)
							newTarget, err := pubsub.ParsePID(command.TargetPID)
							if err != nil {
								connLogger.Error("Invalid target PID in control command", zap.Error(err))
								continue
							}

							if oldTarget.String() != newTarget.String() {
								// Send leave to old target
								leaveMsg := pubsub.NewPackage(wsPID, oldTarget, WSLeaveTopic,
									payload.NewString(wsPID.String()))
								if err := node.Send(leaveMsg); err != nil {
									connLogger.Error("Error sending leave on target change", zap.Error(err))
								}

								// Send join to new target
								joinMsg := pubsub.NewPackage(wsPID, newTarget, WSJoinTopic,
									payload.NewString(wsPID.String()))
								if err := node.Send(joinMsg); err != nil {
									connLogger.Error("Error sending join on target change", zap.Error(err))
								}
							}

							currentTargetPID.Store(newTarget)
							connLogger.Info("Updated target PID", zap.String("newTarget", newTarget.String()))
						}

						// Update message topic if provided
						if command.MessageTopic != "" {
							currentMessageTopic.Store(pubsub.Topic(command.MessageTopic))
							connLogger.Info("Updated message topic", zap.String("newTopic", command.MessageTopic))
						}

						// Update heartbeat interval if provided
						if command.HeartbeatInterval != "" {
							if interval, err := time.ParseDuration(command.HeartbeatInterval); err == nil {
								heartbeatInterval = interval
								heartbeatTicker.Reset(heartbeatInterval)
								connLogger.Info("Updated heartbeat interval", zap.Duration("newInterval", heartbeatInterval))
							}
						}

						continue
					}

					// Handle close messages
					if msg.Topic == WSCloseTopic {
						reason := "Connection closed by server"
						code := websocket.StatusNormalClosure

						// todo: release 						pubsub.ReleasePackage(pkg)!!!

						if len(msg.Payloads) > 0 {
							// Try to extract reason and code from payload
							payload := msg.Payloads[0].Data()
							if str, ok := payload.(string); ok {
								reason = str
							} else if data, err := json.Marshal(payload); err == nil {
								reason = string(data)
							}
						}

						connLogger.Info("Received close command", zap.String("reason", reason))
						conn.Close(code, reason)
						return
					}

					// Forward regular messages to WebSocket
					if len(msg.Payloads) > 0 {
						p := msg.Payloads[0]
						var msgType = websocket.MessageText

						// Process different payload types
						switch p.Format() {
						case payload.String:
							// Handle string payloads directly
							if str, ok := p.Data().(string); ok {
								if err := conn.Write(wsCtx, msgType, []byte(str)); err != nil {
									connLogger.Error("Error writing string to WebSocket", zap.Error(err))
									return
								}
							} else {
								// Convert to string if not already
								strData := fmt.Sprintf("%v", p.Data())
								if err := conn.Write(wsCtx, msgType, []byte(strData)); err != nil {
									connLogger.Error("Error writing converted string to WebSocket", zap.Error(err))
									return
								}
							}

						case payload.JSON:
							// For JSON payloads, marshal directly to bytes
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
									connLogger.Error("Error marshaling JSON payload", zap.Error(err))
									continue
								}
							}

							// Send the JSON directly to WebSocket
							if err := conn.Write(wsCtx, msgType, data); err != nil {
								connLogger.Error("Error writing JSON to WebSocket", zap.Error(err))
								return
							}

						case payload.Bytes:
							// Handle binary data
							if bytes, ok := p.Data().([]byte); ok {
								if err := conn.Write(wsCtx, websocket.MessageBinary, bytes); err != nil {
									connLogger.Error("Error writing binary data to WebSocket", zap.Error(err))
									return
								}
							} else {
								connLogger.Error("Expected bytes payload but got different type")
								continue
							}

						default:
							pj, err := dtt.Transcode(p, payload.JSON)
							if err != nil {
								connLogger.Error("Failed to transcode payload to JSON", zap.Error(err))
								continue
							}

							if err := conn.Write(wsCtx, msgType, pj.Data().([]byte)); err != nil {
								connLogger.Error("Error writing converted JSON to WebSocket", zap.Error(err))
								return
							}
						}
					}
				}
			}
		}
	}()

	// Wait for goroutines to complete
	wg.Wait()

	// Send leave notification
	leaveMsg := pubsub.NewPackage(wsPID, currentTargetPID.Load().(pubsub.PID), WSLeaveTopic, payload.NewString(wsPID.String()))
	if err := node.Send(leaveMsg); err != nil {
		connLogger.Error("Error sending leave message", zap.Error(err))
	}

	// Clean up
	host.Detach(wsPID)
	connLogger.Info("websocket connection closed")
}
