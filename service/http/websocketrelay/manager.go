package websocketrelay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/wippyai/runtime/api/topology"

	"github.com/coder/websocket"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	httpapi "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/internal/uniqid"
	"go.uber.org/zap"
)

const (
	OptionAllowedOrigins = "allowed_origins"
)

// RelayManager manages WebSocket connections and their relay to the relay system
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

// CreateMiddleware creates a configurable WebSocket relay middleware
func (m *RelayManager) CreateMiddleware(options map[string]string) func(http.Handler) http.Handler {
	// Parse allowed origins from options (comma-separated)
	allowedOrigins := options[OptionAllowedOrigins]
	if allowedOrigins == "" {
		allowedOrigins = "*"
	}

	// Split by comma and trim spaces
	var originPatterns []string
	for _, origin := range strings.Split(allowedOrigins, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			originPatterns = append(originPatterns, origin)
		}
	}

	// If no patterns after parsing, default to wildcard
	if len(originPatterns) == 0 {
		originPatterns = []string{"*"}
	}

	return func(h http.Handler) http.Handler {
		return m.middlewareWithOrigins(h, originPatterns)
	}
}

// Middleware creates an HTTP middleware function with default wildcard origin
func (m *RelayManager) Middleware(h http.Handler) http.Handler {
	return m.middlewareWithOrigins(h, []string{"*"})
}

// middlewareWithOrigins creates the actual middleware handler with specified origin patterns
func (m *RelayManager) middlewareWithOrigins(h http.Handler, originPatterns []string) http.Handler {
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
			zap.String("remote_addr", r.RemoteAddr),
		)

		// Parse the relay configuration
		var config RelayCommand
		if err := json.Unmarshal([]byte(relayConfigStr), &config); err != nil {
			logger.Error("Invalid relay configuration", zap.Error(err))
			return
		}

		// Parse the target PID
		targetPID, err := relay.ParsePID(config.TargetPID)
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

		// Get dependencies from request context
		// Get the HTTP server's host from FrameContext
		// (not the app-level pubsub host which belongs to the process host service)
		fc := contextapi.FrameFromContext(r.Context())
		if fc == nil {
			errMsg := "FrameContext not found"
			logger.Error(errMsg)
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}

		hostVal, ok := fc.Get(httpapi.ContextHost)
		if !ok {
			errMsg := "HTTP server host not found in context"
			logger.Error(errMsg)
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}

		relayHost, ok := hostVal.(relay.Host)
		if !ok {
			errMsg := fmt.Sprintf("Invalid host type: %T", hostVal)
			logger.Error(errMsg)
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}

		node := relay.GetNode(r.Context())
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

		topo := topology.GetTopology(r.Context())
		if topo == nil {
			errMsg := "Topology not found in context"
			logger.Error(errMsg)
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}

		// Get server ID from FrameContext
		serverIDVal, ok := fc.Get(httpapi.ContextServerID)
		if !ok {
			logger.Error("Server ID not found in context")
			http.Error(w, "Server ID not found in context", http.StatusInternalServerError)
			return
		}

		serverID, ok := serverIDVal.(registry.ID)
		if !ok || serverID.String() == "" {
			logger.Error("Invalid server ID in context")
			http.Error(w, "Server ID not found in context", http.StatusInternalServerError)
			return
		}
		// Upgrade the connection to WebSocket
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: originPatterns,
		})
		if err != nil {
			logger.Error("Error upgrading to WebSocket", zap.Error(err))
			return
		}

		// Create a context for the WebSocket connection based on the application context
		// This ensures it won't be canceled when the HTTP request is done
		wsCtx, wsFC := contextapi.OpenFrameContext(m.appCtx)
		if err := wsFC.Set(httpapi.ContextServerID, serverID); err != nil {
			logger.Error("Failed to set server ID in frame context", zap.Error(err))
			if closeErr := conn.Close(websocket.StatusInternalError, "Failed to set server ID"); closeErr != nil {
				logger.Error("Error closing WebSocket connection", zap.Error(closeErr))
			}
			return
		}

		// Create and start a new connection with the idGen from the RelayManager
		wsConn, err := NewConnection(
			wsCtx,
			conn,
			targetPID,
			config,
			messageTopic,
			serverID,
			relayHost,
			node,
			topo,
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
