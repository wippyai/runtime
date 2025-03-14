package websocket_relay

import (
	"context"
	"encoding/json"
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

	// WSControlTopic is the topic for configuration
	WSControlTopic pubsub.Topic = "ws.control"

	// WSCloseTopic is the topic for close signals (any message to close the connection)
	WSCloseTopic pubsub.Topic = "ws.close"
)

// RelayCommand holds the configuration for a WebSocket relay request, can be send at start or into ws.control
type RelayCommand struct {
	// TargetPID is the Target that should receive WebSocket messages
	TargetPID string `json:"target_pid"`

	// MessageTopic is the topic to use for WebSocket messages (optional)
	MessageTopic string `json:"message_topic,omitempty"`
}

// RelayManager manages WebSocket connections and their relay to the pubsub system
type RelayManager struct {
	logger *zap.Logger
	idGen  *uniqid.Generator
}

// NewWebSocketRelay creates a new WebSocket relay manager
func NewWebSocketRelay(logger *zap.Logger) *RelayManager {
	return &RelayManager{
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
			http.Error(w, "Invalid relay configuration: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Parse the target Target
		targetPID, err := pubsub.ParsePID(config.TargetPID)
		if err != nil {
			logger.Error("Invalid target Target", zap.Error(err), zap.String("pid", config.TargetPID))
			http.Error(w, "Invalid target Target: "+err.Error(), http.StatusBadRequest)
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
		go m.handleConnection(r.Context(), conn, targetPID, messageTopic)
	})
}

// safeClose closes a WebSocket connection and logs any errors
func (m *RelayManager) safeClose(conn *websocket.Conn, code websocket.StatusCode, reason string, logger *zap.Logger) {
	if err := conn.Close(code, reason); err != nil {
		logger.Error("Error closing WebSocket connection",
			zap.Error(err),
			zap.Int("statusCode", int(code)),
			zap.String("reason", reason))
	}
}

// handleConnection manages a WebSocket connection and its bidirectional communication
func (m *RelayManager) handleConnection(
	ctx context.Context,
	conn *websocket.Conn,
	targetPID pubsub.PID,
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
		m.safeClose(conn, websocket.StatusInternalError, "Host not found in context", connLogger)
		return
	}

	// Get node from context
	node := pubsub.GetNode(ctx)
	if node == nil {
		connLogger.Error("Node not found in context")
		m.safeClose(conn, websocket.StatusInternalError, "Node not found in context", connLogger)
		return
	}

	nodeID := ""

	// Get server ID from context
	serverID, ok := ctx.Value(httpapi.ContextServerID).(registry.ID)
	if !ok || serverID.String() == "" {
		connLogger.Error("Server ID not found in context")
		m.safeClose(conn, websocket.StatusInternalError, "Server ID not found in context", connLogger)
		return
	}
	hostID := serverID.String()

	// Get transcoder from context
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		connLogger.Error("Transcoder not found in context")
		m.safeClose(conn, websocket.StatusInternalError, "Transcoder not found in context", connLogger)
		return
	}

	// Create a unique Target for this WebSocket connection
	wsPID := pubsub.PID{
		Node:   nodeID,
		Host:   hostID,
		ID:     registry.ParseID("ws:conn"),
		UniqID: m.idGen.Generate(),
	}

	// Update logger with WebSocket ID
	connLogger = connLogger.With(zap.String("pid", wsPID.String()))
	connLogger.Info("WebSocket connection established")

	// Create a channel for receiving messages from pubsub
	msgCh := make(chan *pubsub.Package, 10)

	// Attach the WebSocket Target to the relay host
	cancel, err := host.Attach(wsPID, msgCh)
	if err != nil {
		connLogger.Error("Error attaching WebSocket to relay host", zap.Error(err))
		m.safeClose(conn, websocket.StatusInternalError, "Failed to attach to relay", connLogger)
		return
	}
	defer cancel()

	// Create a context with cancellation for the WebSocket handlers
	wsCtx, wsCancel := context.WithCancel(context.Background())
	defer wsCancel()

	// Atomic variables for configuration changes
	var currentTargetPID atomic.Value
	var currentMessageTopic atomic.Value

	currentTargetPID.Store(targetPID)
	currentMessageTopic.Store(messageTopic)

	// Send a join notification to the target Target
	joinMsg := pubsub.NewPackage(wsPID, targetPID, WSJoinTopic, payload.New(wsPID))
	if err := node.Send(joinMsg); err != nil {
		connLogger.Error("Error sending join message", zap.Error(err))
		m.safeClose(conn, websocket.StatusInternalError, "Error sending join message", connLogger)
		return
	}

	// Create a WaitGroup for the main handlers
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
					// Handle control messages (reconfiguration)
					if msg.Topic == WSControlTopic && len(msg.Payloads) > 0 {
						var command RelayCommand
						if err := dtt.Unmarshal(msg.Payloads[0], &command); err != nil {
							connLogger.Error("Failed to unmarshal control payload", zap.Error(err))
							continue
						}

						// Update configuration
						if command.TargetPID != "" {
							newTargetPID, err := pubsub.ParsePID(command.TargetPID)
							if err != nil {
								connLogger.Error("Invalid target Target in control command",
									zap.Error(err),
									zap.String("pid", command.TargetPID))
								continue
							}
							currentTargetPID.Store(newTargetPID)
							connLogger.Info("Updated target Target", zap.String("newTargetPID", newTargetPID.String()))
						}

						if command.MessageTopic != "" {
							currentMessageTopic.Store(pubsub.Topic(command.MessageTopic))
							connLogger.Info("Updated message topic", zap.String("newTopic", command.MessageTopic))
						}

						continue
					}

					// Handle close messages
					if msg.Topic == WSCloseTopic {
						connLogger.Info("Received close command from server")
						wsCancel() // Trigger clean shutdown
						return
					}

					// Forward regular messages to WebSocket
					if len(msg.Payloads) > 0 {
						p := msg.Payloads[0]
						var data []byte
						var msgType = websocket.MessageText

						// Use DTT to transcode to the appropriate format
						var jsonPayload payload.Payload
						var err error

						// Convert to JSON if not already in JSON or string format
						if p.Format() != payload.JSON && p.Format() != payload.String {
							jsonPayload, err = dtt.Transcode(p, payload.JSON)
							if err != nil {
								connLogger.Error("Error transcoding payload", zap.Error(err))
								continue
							}
						} else {
							jsonPayload = p
						}

						// Extract data based on format
						switch jsonPayload.Format() {
						case payload.JSON:
							data = []byte(jsonPayload.Data().(string))
						case payload.String:
							data = []byte(jsonPayload.Data().(string))
						case payload.Bytes:
							data = jsonPayload.Data().([]byte)
							msgType = websocket.MessageBinary
						default:
							connLogger.Error("Unsupported payload format after transcoding",
								zap.String("format", string(jsonPayload.Format())))
							continue
						}

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
					payloadData = payload.NewString(string(data))
				} else {
					payloadData = payload.NewPayload(data, payload.Bytes)
				}

				// Get current configuration values
				currentTarget := currentTargetPID.Load().(pubsub.PID)
				currentTopic := currentMessageTopic.Load().(pubsub.Topic)

				// Send to target Target using the current message topic
				msg := pubsub.NewPackage(wsPID, currentTarget, currentTopic, payloadData)
				if err := node.Send(msg); err != nil {
					connLogger.Error("Error sending to pubsub", zap.Error(err))
					wsCancel()
					return
				}
			}
		}
	}()

	// Wait for handlers to complete
	wg.Wait()

	// Send a leave notification to the target Target
	leaveMsg := pubsub.NewPackage(wsPID, currentTargetPID.Load().(pubsub.PID), WSLeaveTopic, payload.New(wsPID))
	if err := node.Send(leaveMsg); err != nil {
		connLogger.Error("Error sending leave message", zap.Error(err))
	}

	// Clean up
	host.Detach(wsPID)
	m.safeClose(conn, websocket.StatusNormalClosure, "Connection closed", connLogger)
	connLogger.Info("WebSocket connection closed")
}

type responseWrapper struct {
	http.ResponseWriter
	headers http.Header
}

func newResponseWrapper(w http.ResponseWriter) *responseWrapper {
	return &responseWrapper{
		ResponseWriter: w,
		headers:        w.Header(),
	}
}

func (rw *responseWrapper) Header() http.Header {
	return rw.headers
}
