package websocket_relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/coder/websocket"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	httpapi "github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/internal/uniqid"
	"go.uber.org/zap"
)

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

		// Get dependencies from request context
		host := pubsub.GetHost(r.Context())
		if host == nil {
			errMsg := "Host not found in context"
			logger.Error(errMsg)
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}

		node := pubsub.GetNode(r.Context())
		if node == nil {
			errMsg := "Node not found in context"
			logger.Error(errMsg)
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}

		transcoder := payload.GetTranscoder(r.Context())
		if transcoder == nil {
			errMsg := "Transcoder not found in context"
			logger.Error(errMsg)
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}

		// Get server ID from context
		serverID, ok := r.Context().Value(httpapi.ContextServerID).(registry.ID)
		if !ok || serverID.String() == "" {
			logger.Error("Server ID not found in context")
			http.Error(w, "Server ID not found in context", http.StatusInternalServerError)
			return
		}

		// Upgrade the connection to WebSocket
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			logger.Error("Error upgrading to WebSocket", zap.Error(err))
			return
		}

		// Create a context for the WebSocket connection based on the application context
		// This ensures it won't be canceled when the HTTP request is done
		wsCtx := context.WithValue(m.appCtx, httpapi.ContextServerID, serverID)

		// Create and start a new connection with the idGen from the RelayManager
		wsConn, err := NewConnection(
			wsCtx,
			conn,
			targetPID,
			config,
			messageTopic,
			serverID,
			host,
			node,
			transcoder,
			m.idGen,
			logger,
		)

		if err != nil {
			errMsg := fmt.Sprintf("Error creating WebSocket connection: %v", err)
			logger.Error(errMsg, zap.Error(err))
			if closeErr := conn.Close(websocket.StatusInternalError, errMsg); closeErr != nil {
				logger.Error("Error closing WebSocket connection", zap.Error(closeErr))
			}
			return
		}

		// Handle the WebSocket connection in a separate goroutine
		wsConn.Serve()
	})
}
