package wsrelay

import (
	"context"
	"encoding/json"
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
	// Option keys (dot-separated, preferred)
	OptionAllowedOrigins = "wsrelay.allowed.origins"

	// Legacy option keys (deprecated, for backward compatibility)
	legacyAllowedOrigins = "allowed_origins"
)

// getOption retrieves an option value, checking the new dot-separated key first,
// then falling back to the legacy underscore key for backward compatibility
func getOption(options map[string]string, newKey, legacyKey string) string {
	if val, ok := options[newKey]; ok {
		return val
	}
	return options[legacyKey]
}

// RelayManager manages WebSocket connections and their relay to the relay system
type RelayManager struct {
	appCtx     context.Context
	logger     *zap.Logger
	pidGen     *uniqid.PIDGenerator
	node       relay.Node
	topo       topology.Topology
	transcoder payload.Transcoder
}

// NewWebSocketRelay creates a new WebSocket relay manager
func NewWebSocketRelay(ctx context.Context, logger *zap.Logger, pidGen *uniqid.PIDGenerator) *RelayManager {
	return &RelayManager{
		appCtx:     ctx,
		logger:     logger,
		pidGen:     pidGen,
		node:       relay.GetNode(ctx),
		topo:       topology.GetTopology(ctx),
		transcoder: payload.GetTranscoder(ctx),
	}
}

// CreateMiddleware creates a configurable WebSocket relay middleware
func (m *RelayManager) CreateMiddleware(options map[string]string) func(http.Handler) http.Handler {
	// Parse allowed origins from options (comma-separated)
	allowedOrigins := getOption(options, OptionAllowedOrigins, legacyAllowedOrigins)
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
		return m.middlewareHandler(h, originPatterns)
	}
}

// middlewareHandler creates the actual middleware handler with specified origin patterns
func (m *RelayManager) middlewareHandler(h http.Handler, originPatterns []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrappedWriter := newResponseWrapper(w)
		h.ServeHTTP(wrappedWriter, r)

		relayConfigStr := wrappedWriter.Header().Get(WSRelayHeader)
		if relayConfigStr == "" {
			return
		}
		w.Header().Del(WSRelayHeader)

		logger := m.logger.With(
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr),
		)

		var config RelayCommand
		if err := json.Unmarshal([]byte(relayConfigStr), &config); err != nil {
			logger.Error("Invalid relay configuration", zap.Error(err))
			return
		}

		targetPID, err := relay.ParsePID(config.TargetPID)
		if err != nil {
			logger.Error("Invalid target PID", zap.Error(err), zap.String("pid", config.TargetPID))
			http.Error(w, "Invalid target PID: "+err.Error(), http.StatusBadRequest)
			return
		}

		messageTopic := WSMessageTopic
		if config.MessageTopic != "" {
			messageTopic = config.MessageTopic
		}

		// Get host and serverID from request context
		fc := contextapi.FrameFromContext(r.Context())
		if fc == nil {
			logger.Error("frame context not found")
			http.Error(w, ErrFrameContextNotFound.Error(), http.StatusInternalServerError)
			return
		}

		hostVal, ok := fc.Get(httpapi.ServerCtxKey())
		if !ok {
			logger.Error("server host not found in context")
			http.Error(w, ErrServerHostNotFound.Error(), http.StatusInternalServerError)
			return
		}

		host, ok := hostVal.(relay.AttachableHost)
		if !ok {
			logger.Error("server host does not implement AttachableHost")
			http.Error(w, ErrHostNotAttachable.Error(), http.StatusInternalServerError)
			return
		}

		serverIDVal, ok := fc.Get(httpapi.ServerIDCtxKey())
		if !ok {
			logger.Error("server ID not found in context")
			http.Error(w, ErrServerIDNotFound.Error(), http.StatusInternalServerError)
			return
		}

		var serverID registry.ID
		switch v := serverIDVal.(type) {
		case registry.ID:
			serverID = v
		case string:
			serverID = registry.ParseID(v)
		default:
			logger.Error("invalid server ID type in context")
			http.Error(w, ErrInvalidServerID.Error(), http.StatusInternalServerError)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: originPatterns,
		})
		if err != nil {
			logger.Error("Error upgrading to WebSocket", zap.Error(err))
			return
		}

		wsCtx, wsFC := contextapi.OpenFrameContext(m.appCtx)
		if err := wsFC.Set(httpapi.ServerIDCtxKey(), serverID); err != nil {
			logger.Error("Failed to set server ID in frame context", zap.Error(err))
			_ = conn.Close(websocket.StatusInternalError, "Failed to set server ID")
			return
		}

		wsConn, err := NewConnection(
			wsCtx,
			conn,
			targetPID,
			config,
			messageTopic,
			serverID,
			host,
			m.node,
			m.topo,
			m.transcoder,
			m.pidGen,
			logger,
		)
		if err != nil {
			logger.Error("Error creating WebSocket connection", zap.Error(err))
			_ = conn.Close(websocket.StatusInternalError, err.Error())
			return
		}

		wsConn.Serve()
	})
}
