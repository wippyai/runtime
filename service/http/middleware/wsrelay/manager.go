package wsrelay

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	httpapi "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
	"go.uber.org/zap"
)

// responseWrapper wraps http.ResponseWriter to capture headers before passing to handlers
type responseWrapper struct {
	http.ResponseWriter
	headers    http.Header
	statusCode int
}

func newResponseWrapper(w http.ResponseWriter) *responseWrapper {
	return &responseWrapper{
		ResponseWriter: w,
		headers:        w.Header(),
		statusCode:     200,
	}
}

func (rw *responseWrapper) Header() http.Header {
	return rw.headers
}

func (rw *responseWrapper) Write(data []byte) (int, error) {
	return rw.ResponseWriter.Write(data)
}

func (rw *responseWrapper) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWrapper) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

const (
	// OptionAllowedOrigins is an option key (dot-separated, preferred)
	OptionAllowedOrigins = "wsrelay.allowed.origins"

	// Shared option key (can be used across modules)
	sharedAllowOrigins = "allow_origins"

	// Legacy option keys (deprecated, for backward compatibility)
	legacyAllowedOrigins = "allowed_origins"
)

// getOrigins retrieves allowed origins, checking in order:
// 1. wsrelay.allowed.origins (module-specific)
// 2. allow_origins (shared across modules)
// 3. allowed_origins (legacy)
func getOrigins(options map[string]string) string {
	if val, ok := options[OptionAllowedOrigins]; ok {
		return val
	}
	if val, ok := options[sharedAllowOrigins]; ok {
		return val
	}
	return options[legacyAllowedOrigins]
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
	// Checks: wsrelay.allowed.origins -> allow_origins -> allowed_origins
	allowedOrigins := getOrigins(options)

	// Split by comma and trim spaces
	var originPatterns []string
	for _, origin := range strings.Split(allowedOrigins, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			originPatterns = append(originPatterns, origin)
		}
	}

	// If no patterns configured, log warning but allow same-origin only
	// This is safer than defaulting to "*" which allows any origin
	if len(originPatterns) == 0 {
		m.logger.Warn("wsrelay: no allowed origins configured, using same-origin only")
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

		relayConfigStr := wrappedWriter.Header().Get(RelayHeader)
		if relayConfigStr == "" {
			return
		}
		wrappedWriter.Header().Del(RelayHeader)

		logger := m.logger.With(
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr),
		)

		var config RelayCommand
		if err := json.Unmarshal([]byte(relayConfigStr), &config); err != nil {
			logger.Error("Invalid relay configuration", zap.Error(err))
			return
		}

		targetPID, err := pid.ParsePID(config.TargetPID)
		if err != nil {
			logger.Error("Invalid target PID", zap.Error(err), zap.String("pid", config.TargetPID))
			http.Error(w, "Invalid target PID: "+err.Error(), http.StatusBadRequest)
			return
		}

		messageTopic := MessageTopic
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

		hostVal, ok := fc.Get(httpapi.ServerKey())
		if !ok {
			logger.Error("server host not found in context")
			http.Error(w, ErrServerHostNotFound.Error(), http.StatusInternalServerError)
			return
		}

		host, ok := hostVal.(relay.AttachableReceiver)
		if !ok {
			logger.Error("server host does not implement AttachableReceiver")
			http.Error(w, ErrHostNotAttachable.Error(), http.StatusInternalServerError)
			return
		}

		serverIDVal, ok := fc.Get(httpapi.ServerIDKey())
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
		if err := wsFC.Set(httpapi.ServerIDKey(), serverID); err != nil {
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
