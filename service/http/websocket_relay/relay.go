package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/topology"
	"go.uber.org/zap"
)

// Constants for the WebSocket relay
const (
	// WSRelayHeader is the header that indicates a WebSocket connection request, send by downstream
	WSRelayHeader = "X-WS-Relay"

	// WSMessageTopic is the default topic for WebSocket messages
	WSMessageTopic pubsub.Topic = "ws.message"

	// WSJoinTopic is the topic for join notifications
	WSJoinTopic pubsub.Topic = "ws.join"

	// WSLeaveTopic is the topic for leave notifications
	WSLeaveTopic pubsub.Topic = "ws.leave"
)

// RelayConfig holds the configuration for a WebSocket relay request
type RelayConfig struct {
	// TargetPID is the PID that should receive WebSocket messages
	TargetPID string `json:"target_pid"`

	// MessageTopic is the topic to use for WebSocket messages (optional)
	MessageTopic string `json:"message_topic,omitempty"`
}

// RelayManager manages WebSocket connections and their relay to the pubsub system
type RelayManager struct {
	// host is the pubsub host used for message routing
	host pubsub.Host

	// logger is the structured logger for this manager
	logger *zap.Logger

	// activeConns tracks active WebSocket connections
	activeConns sync.Map
}

// NewWebSocketRelayManager creates a new WebSocket relay manager
func NewWebSocketRelayManager(host pubsub.Host, logger *zap.Logger) *RelayManager {
	return &RelayManager{
		host:   host,
		logger: logger,
	}
}

// Middleware creates an HTTP middleware function that handles WebSocket relay requests
func (m *RelayManager) Middleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)

		relayConfigStr := r.Header.Get(WSRelayHeader)
		if relayConfigStr == "" {
			return // not out business
		}

		// Get context logger and add request info
		logger := m.logger.With(
			zap.String("path", r.URL.Path),
			zap.String("remoteAddr", r.RemoteAddr),
		)

		logger.Debug("Handling WebSocket relay request", zap.String("config", relayConfigStr))

		// Parse the relay configuration
		var config RelayConfig
		if err := json.Unmarshal([]byte(relayConfigStr), &config); err != nil {
			logger.Error("Invalid relay configuration", zap.Error(err))
			http.Error(w, "Invalid relay configuration: "+err.Error(), http.StatusBadRequest)
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
			messageTopic = config.MessageTopic
		}

		// Upgrade the connection to WebSocket
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			logger.Error("Error upgrading to WebSocket", zap.Error(err))
			return
		}

		// Handle the WebSocket connection
		go m.handleConnection(r.Context(), conn, targetPID, messageTopic)
	})
}

// handleConnection manages a WebSocket connection and its bidirectional communication
func (m *RelayManager) handleConnection(
	ctx context.Context,
	conn *websocket.Conn,
	targetPID pubsub.PID,
	messageTopic string,
) {
	// todo: use uniq id

	// Create a unique PID for this WebSocket connection
	wsPID := pubsub.PID{
		Node:   targetPID.Node,
		Host:   "ws-relay",
		ID:     registry.ParseID("ws:connection"),
		UniqID: time.Now().Format("20060102150405.000000000"), // todo: use uniq id generator
	}

	connLogger := m.logger.With(
		zap.String("wsID", wsPID.String()),
		zap.String("targetPID", targetPID.String()),
		zap.String("topic", messageTopic),
	)

	connLogger.Info("WebSocket connection established")

	// Track the active connection
	m.activeConns.Store(wsPID.String(), conn)
	defer m.activeConns.Delete(wsPID.String()) // todo: do i need it?

	// Create a channel for receiving messages from pubsub
	msgCh := make(chan *pubsub.Package, 10)

	// Attach the WebSocket PID to the relay host
	cancel, err := m.host.Attach(wsPID, msgCh)
	if err != nil {
		connLogger.Error("Error attaching WebSocket to relay host", zap.Error(err))
		conn.Close(websocket.StatusInternalError, "Failed to attach to relay")
		return
	}
	defer cancel()

	// Send a join notification to the target PID
	joinMsg := pubsub.NewPackage(targetPID, WSJoinTopic, payload.New(wsPID))

	if err := m.host.Send(joinMsg); err != nil {
		connLogger.Error("Error sending join message", zap.Error(err))
		// todo: always handle close errors
		conn.Close(websocket.StatusInternalError, "Failed to send join message")
		return
	}

	// Create a context with cancellation for the WebSocket handlers
	wsCtx, wsCancel := context.WithCancel(ctx)
	defer wsCancel()

	// Create a WaitGroup for the handlers
	var wg sync.WaitGroup
	wg.Add(2)

	// Start handler for messages from pubsub to WebSocket
	go func() {
		defer wg.Done()

		for {
			select {
			case <-wsCtx.Done():
				return
			case pkg, ok := <-msgCh:
				if !ok {
					connLogger.Debug("Message channel closed")
					return
				}

				for _, msg := range pkg.Messages {
					// Check for topology events (cancel, exit)
					if msg.Topic == topology.TopicEvents {
						// todo: handle it properly
						continue
					}

					// Forward regular messages to WebSocket
					if len(pkg.Messages) > 0 && len(pkg.Messages[0].Payloads) > 0 {
						p := pkg.Messages[0].Payloads[0]
						var data []byte
						var msgType = websocket.MessageText // todo: support binaries too later

						// todo: wrong use dtt!!!!!!

						// Convert payload to appropriate format for WebSocket
						switch p.Format() {
						case payload.JSON:
							data = []byte(p.Data().(string))
						case payload.String:
							data = []byte(p.Data().(string))
						case payload.Golang:
							jsonData, err := json.Marshal(p.Data())
							if err != nil {
								connLogger.Error("Error marshaling payload", zap.Error(err))
								continue
							}
							data = jsonData
						case payload.Bytes:
							data = p.Data().([]byte)
							msgType = websocket.MessageBinary
						default:

							jsonData, err := json.Marshal(p.Data())
							if err != nil {
								connLogger.Error("Error marshaling payload", zap.Error(err))
								continue
							}
							data = jsonData
						}

						// todo: above is wrong, use dtt

						// Write to WebSocket
						if err := conn.Write(wsCtx, msgType, data); err != nil {
							connLogger.Error("Error writing to WebSocket", zap.Error(err))
							wsCancel()
							return
						}
					}
				}
			}
		}
	}()

	// Start handler for messages from WebSocket to pubsub
	go func() {
		defer wg.Done()

		for {
			select {
			case <-wsCtx.Done():
				return
			default:
				// Read from WebSocket with context
				msgType, data, err := conn.Read(wsCtx)
				if err != nil {
					if closeStatus := websocket.CloseStatus(err); closeStatus != -1 {
						connLogger.Info("WebSocket closed by client",
							zap.Int("closeCode", int(closeStatus)),
							zap.String("error", err.Error()))
					} else {
						connLogger.Error("Error reading from WebSocket", zap.Error(err))
					}
					wsCancel()
					return
				}

				// Convert to pubsub payload
				var payloadData payload.Payload

				if msgType == websocket.MessageText {
					// Try to parse as JSON
					var jsonData interface{}
					if json.Unmarshal(data, &jsonData) == nil {
						payloadData = payload.NewPayload(data, payload.JSON)
					} else {
						// Treat as plain text ??? todo test it
						payloadData = payload.NewString(string(data))
					}
				} else {
					// Binary data
					payloadData = payload.NewPayload(data, payload.Bytes)
				}

				// Send to target PID using the specified topic
				msg := pubsub.NewPackage(targetPID, messageTopic, payloadData)
				if err := m.host.Send(msg); err != nil {
					connLogger.Error("Error sending to pubsub", zap.Error(err))
					wsCancel()
					return
				}
			}
		}
	}()

	// Wait for handlers to complete
	wg.Wait()

	// Send a leave notification to the target PID
	leaveMsg := pubsub.NewPackage(targetPID, WSLeaveTopic, payload.New(wsPID))
	if err := m.host.Send(leaveMsg); err != nil {
		connLogger.Error("Error sending leave message", zap.Error(err))
	}

	// Clean up
	m.host.Detach(wsPID)
	conn.Close(websocket.StatusNormalClosure, "Connection closed")
	connLogger.Info("WebSocket connection closed")
}

// Active returns the count of active WebSocket connections
func (m *RelayManager) Active() int {
	count := 0
	m.activeConns.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}
