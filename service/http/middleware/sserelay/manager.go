// SPDX-License-Identifier: MPL-2.0

package sserelay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"

	contextapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	httpapi "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

type sessionRunner interface {
	Serve(reqCtx context.Context, w http.ResponseWriter) error
	Close(reason string)
}

type sessionFactory func(
	appCtx context.Context,
	config RelayCommand,
	serverID registry.ID,
	host relay.AttachableReceiver,
	node relay.Node,
	topo topology.Topology,
	transcoder payload.Transcoder,
	pidGen process.PIDGenerator,
	logger *zap.Logger,
) (sessionRunner, error)

// responseWrapper captures write state while preserving normal writer behavior.
type responseWrapper struct {
	http.ResponseWriter
	headers     http.Header
	statusCode  int
	wroteHeader bool
	wroteBody   bool
}

func newResponseWrapper(w http.ResponseWriter) *responseWrapper {
	return &responseWrapper{
		ResponseWriter: w,
		headers:        w.Header(),
		statusCode:     http.StatusOK,
	}
}

func (rw *responseWrapper) Header() http.Header {
	return rw.headers
}

func (rw *responseWrapper) Write(data []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	rw.wroteBody = true
	return rw.ResponseWriter.Write(data)
}

func (rw *responseWrapper) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWrapper) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// RelayManager manages detached SSE relay sessions.
type RelayManager struct {
	appCtx     context.Context
	logger     *zap.Logger
	pidGen     process.PIDGenerator
	node       relay.Node
	topo       topology.Topology
	transcoder payload.Transcoder
	newSession sessionFactory
}

// NewSSERelay creates a new detached SSE relay manager.
func NewSSERelay(ctx context.Context, logger *zap.Logger, pidGen process.PIDGenerator) *RelayManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &RelayManager{
		appCtx:     ctx,
		logger:     logger,
		pidGen:     pidGen,
		node:       relay.GetNode(ctx),
		topo:       topology.GetTopology(ctx),
		transcoder: payload.GetTranscoder(ctx),
		newSession: func(
			appCtx context.Context,
			config RelayCommand,
			serverID registry.ID,
			host relay.AttachableReceiver,
			node relay.Node,
			topo topology.Topology,
			transcoder payload.Transcoder,
			pidGen process.PIDGenerator,
			logger *zap.Logger,
		) (sessionRunner, error) {
			return NewSession(appCtx, config, serverID, host, node, topo, transcoder, pidGen, logger)
		},
	}
}

// CreateMiddleware creates an SSE relay middleware.
func (m *RelayManager) CreateMiddleware(options map[string]string) func(http.Handler) http.Handler {
	allowedOrigins := getOrigins(options)
	var originPatterns []string
	for _, origin := range strings.Split(allowedOrigins, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			originPatterns = append(originPatterns, origin)
		}
	}

	if len(originPatterns) == 0 {
		m.logger.Warn("sserelay: no allowed origins configured, using same-origin only")
	}

	return func(h http.Handler) http.Handler {
		return m.middlewareHandler(h, originPatterns)
	}
}

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

		if wrappedWriter.wroteBody {
			logger.Warn("cannot start detached sse relay: response body already written")
			return
		}
		if wrappedWriter.wroteHeader && wrappedWriter.statusCode != http.StatusOK {
			logger.Warn("cannot start detached sse relay: non-200 status already written",
				zap.Int("status", wrappedWriter.statusCode))
			return
		}

		if !originAllowed(r, originPatterns) {
			http.Error(wrappedWriter, ErrOriginNotAllowed.Error(), http.StatusForbidden)
			return
		}

		var config RelayCommand
		if err := json.Unmarshal([]byte(relayConfigStr), &config); err != nil {
			http.Error(wrappedWriter, "invalid relay configuration: "+err.Error(), http.StatusBadRequest)
			return
		}

		fc := contextapi.FrameFromContext(r.Context())
		if fc == nil {
			http.Error(wrappedWriter, ErrFrameContextNotFound.Error(), http.StatusInternalServerError)
			return
		}

		hostVal, ok := fc.Get(httpapi.ServerKey())
		if !ok {
			http.Error(wrappedWriter, ErrServerHostNotFound.Error(), http.StatusInternalServerError)
			return
		}
		host, ok := hostVal.(relay.AttachableReceiver)
		if !ok {
			http.Error(wrappedWriter, ErrHostNotAttachable.Error(), http.StatusInternalServerError)
			return
		}

		serverIDVal, ok := fc.Get(httpapi.ServerIDKey())
		if !ok {
			http.Error(wrappedWriter, ErrServerIDNotFound.Error(), http.StatusInternalServerError)
			return
		}

		var serverID registry.ID
		switch v := serverIDVal.(type) {
		case registry.ID:
			serverID = v
		case string:
			serverID = registry.ParseID(v)
		default:
			http.Error(wrappedWriter, ErrInvalidServerID.Error(), http.StatusInternalServerError)
			return
		}

		session, err := m.newSession(
			m.appCtx,
			config,
			serverID,
			host,
			m.node,
			m.topo,
			m.transcoder,
			m.pidGen,
			logger,
		)
		if err != nil {
			http.Error(wrappedWriter, err.Error(), httpStatusFromError(err))
			return
		}
		defer session.Close("request finished")

		if err := session.Serve(r.Context(), wrappedWriter); err != nil {
			// If streaming already started, just log and stop.
			if wrappedWriter.wroteHeader || wrappedWriter.wroteBody {
				logger.Debug("sse relay ended with stream error", zap.Error(err))
				return
			}
			http.Error(wrappedWriter, err.Error(), httpStatusFromError(err))
		}
	})
}

func httpStatusFromError(err error) int {
	var apiErr apierror.Error
	if !errors.As(err, &apiErr) {
		return http.StatusInternalServerError
	}

	switch apiErr.Kind() {
	case apierror.Invalid:
		return http.StatusBadRequest
	case apierror.PermissionDenied:
		return http.StatusForbidden
	case apierror.NotFound:
		return http.StatusNotFound
	case apierror.AlreadyExists, apierror.Conflict:
		return http.StatusConflict
	case apierror.Timeout, apierror.Canceled:
		return http.StatusRequestTimeout
	case apierror.RateLimited:
		return http.StatusTooManyRequests
	case apierror.Unavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// getOrigins retrieves allowed origins, checking in order:
// 1. sserelay.allowed.origins (module-specific)
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

func originAllowed(r *http.Request, patterns []string) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	if len(patterns) == 0 {
		return sameOrigin(origin, r)
	}

	for _, p := range patterns {
		if p == "*" {
			return true
		}
		if origin == p {
			return true
		}
		if ok, _ := path.Match(p, origin); ok {
			return true
		}
	}
	return false
}

func sameOrigin(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	if !strings.EqualFold(u.Host, r.Host) {
		return false
	}

	reqScheme := "http"
	if r.TLS != nil {
		reqScheme = "https"
	}
	if xfp := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); xfp != "" {
		reqScheme = strings.ToLower(xfp)
	}

	return strings.EqualFold(u.Scheme, reqScheme)
}
