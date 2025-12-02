package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"

	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	config "github.com/wippyai/runtime/api/service/http"
)

// RequestContext pool to reduce allocations
var requestContextPool = sync.Pool{
	New: func() interface{} {
		return &config.RequestContext{}
	},
}

// getRequestContext gets a RequestContext from pool and initializes it
func getRequestContext(r *http.Request, w http.ResponseWriter) *config.RequestContext {
	ctx := requestContextPool.Get().(*config.RequestContext)

	// Initialize with new request/response
	ctx.SetRequest(r)
	ctx.SetResponseWriter(w)
	ctx.ResetHandled()

	return ctx
}

// putRequestContext returns RequestContext to pool
func putRequestContext(ctx *config.RequestContext) {
	if ctx != nil {
		// Clear references for GC
		ctx.SetRequest(nil)
		ctx.SetResponseWriter(nil)
		ctx.ResetHandled()

		requestContextPool.Put(ctx)
	}
}

// EndpointFactory creates HTTP handlers for function endpoints
type EndpointFactory struct {
	funcs function.Registry
}

// Ensure EndpointFactory implements EndpointFactoryAPI
var _ EndpointFactoryAPI = (*EndpointFactory)(nil)

// NewEndpointFactory creates a new endpoint factory instance with the provided function registry
func NewEndpointFactory(funcs function.Registry) (*EndpointFactory, error) {
	if funcs == nil {
		return nil, fmt.Errorf("function registry is required")
	}
	return &EndpointFactory{
		funcs: funcs,
	}, nil
}

// CreateHandler creates an HTTP handler from the provided endpoint configuration
func (f *EndpointFactory) CreateHandler(_ context.Context, cfg *config.EndpointConfig) (http.Handler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid endpoint config: %w", err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get pooled RequestContext
		rCtx := getRequestContext(r, w)

		// Return to pool when request is done
		defer putRequestContext(rCtx)

		// Use existing context (FrameContext already created in handler wrapper)
		execCtx := r.Context()

		// Seal the frame context before calling the function
		// This allows middleware to set values (actor, etc.) in unsealed frame
		// while preventing request context from leaking to child functions
		fc := contextapi.FrameFromContext(execCtx)
		if fc != nil {
			fc.Seal()
		}

		// Read request body as payload for functions that accept parameters
		var payloads payload.Payloads
		if r.Body != nil && r.ContentLength != 0 {
			body, err := io.ReadAll(r.Body)
			if err == nil && len(body) > 0 {
				payloads = payload.Payloads{payload.NewPayload(body, payload.JSON)}
			}
		}

		// Create task with request context as pairs (not in frame)
		// This prevents request context from leaking to child function calls
		task := runtime.Task{
			ID: cfg.Func,
			Context: []contextapi.Pair{
				{Key: config.RequestCtxKey(), Value: rCtx},
			},
			Payloads: payloads,
		}

		result, err := f.funcs.Call(execCtx, task)
		if err != nil {
			if !rCtx.ResponseHandled() {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		if rCtx.ResponseHandled() {
			return
		}

		if result == nil {
			http.Error(w, "received nil result", http.StatusInternalServerError)
			return
		}
		if result.Error != nil {
			http.Error(w, result.Error.Error(), http.StatusInternalServerError)
			return
		}

		// If function returned a result payload, write it as JSON response
		if result.Value != nil && !rCtx.ResponseHandled() {
			w.Header().Set("Content-Type", "application/json")
			if data, ok := result.Value.Data().([]byte); ok {
				_, _ = w.Write(data)
			}
			return
		}

		if !rCtx.ResponseHandled() {
			http.Error(w, "no response sent", http.StatusInternalServerError)
		}
	}), nil
}

// SPAHandler implements http.Handler to serve a Single Page Application
type SPAHandler struct {
	fs        http.FileSystem
	indexPath string
}

// NewSPAHandler creates a new handler for SPA serving with fallback to index file
func NewSPAHandler(fsys fs.FS, indexPath string) http.Handler {
	return &SPAHandler{
		fs:        http.FS(fsys),
		indexPath: indexPath,
	}
}

// ServeHTTP handles HTTP requests for SPA applications
// It serves static files directly when found, or falls back to the index file
func (h *SPAHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean the path to prevent directory traversal
	urlPath := path.Clean(r.URL.Path)

	// Try serving the exact file first
	if _, err := h.fs.Open(strings.TrimPrefix(urlPath, "/")); err == nil {
		// File exists, serve it directly
		http.FileServer(h.fs).ServeHTTP(w, r)
		return
	}

	// For all other routes, serve the index.html
	file, err := h.fs.Open(h.indexPath)
	if err != nil {
		http.Error(w, "index file not found", http.StatusInternalServerError)
		return
	}
	defer func() { _ = file.Close() }()

	// Get file info for modification time
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "failed to get index stats", http.StatusInternalServerError)
		return
	}

	// Set appropriate headers
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, h.indexPath, info.ModTime(), file)
}

// StaticFactory creates HTTP handlers for static file serving
type StaticFactory struct {
	fsReg             fs.Registry
	middlewareFactory MiddlewareAPI
}

// Ensure StaticFactory implements StaticFactoryAPI
var _ StaticFactoryAPI = (*StaticFactory)(nil)

// NewStaticFactory creates a new static file factory instance with the provided filesystem registry
func NewStaticFactory(fsReg fs.Registry, middlewareFactory MiddlewareAPI) (*StaticFactory, error) {
	if fsReg == nil {
		return nil, fmt.Errorf("filesystem registry is required")
	}
	if middlewareFactory == nil {
		return nil, fmt.Errorf("middleware factory is required")
	}
	return &StaticFactory{
		fsReg:             fsReg,
		middlewareFactory: middlewareFactory,
	}, nil
}

// CreateHandler creates an HTTP handler from the provided static file configuration
func (f *StaticFactory) CreateHandler(_ context.Context, cfg *config.StaticConfig) (http.Handler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid static config: %w", err)
	}

	fsys, ok := f.fsReg.GetFS(cfg.FS.String())
	if !ok {
		return nil, fmt.Errorf("filesystem not found: %s", cfg.FS)
	}

	// Create base handler
	var handler http.Handler

	// For SPA mode, use our custom handler
	if cfg.StaticOptions.SPA {
		if cfg.StaticOptions.IndexFile == "" {
			return nil, fmt.Errorf("index file must be specified for SPA mode")
		}
		handler = NewSPAHandler(fsys, cfg.StaticOptions.IndexFile)

		if cfg.StaticOptions.CacheControl != "" {
			handler = wrapWithCacheControl(handler, cfg.StaticOptions.CacheControl)
		}
	} else {
		handler = http.FileServer(http.FS(fsys))

		if cfg.Directory != "" {
			handler = http.StripPrefix(cfg.Path, handler)
		}

		if cfg.StaticOptions.CacheControl != "" {
			handler = wrapWithCacheControl(handler, cfg.StaticOptions.CacheControl)
		}
	}

	// Apply middleware if configured
	if len(cfg.Middleware) > 0 {
		// Build middleware chain
		middlewareHandlers := make([]func(http.Handler) http.Handler, len(cfg.Middleware))
		for i, name := range cfg.Middleware {
			mw, err := f.middlewareFactory.CreateMiddleware(name, cfg.Options)
			if err != nil {
				return nil, fmt.Errorf("failed to create middleware %s: %w", name, err)
			}
			middlewareHandlers[i] = mw
		}

		// Apply middleware chain in reverse order
		for i := len(middlewareHandlers) - 1; i >= 0; i-- {
			handler = middlewareHandlers[i](handler)
		}
	}

	return handler, nil
}

// ServerFactory creates HTTP server instances
type ServerFactory struct {
	middlewareFactory MiddlewareAPI
}

// NewServerFactory creates a new server factory instance
func NewServerFactory(middlewareFactory MiddlewareAPI) *ServerFactory {
	return &ServerFactory{
		middlewareFactory: middlewareFactory,
	}
}

// CreateServer creates a new HTTP server from the provided configuration
func (f *ServerFactory) CreateServer(id registry.ID, cfg *config.ServerConfig) (Server, error) {
	return NewServerService(id, cfg, f.middlewareFactory)
}

// wrapWithCacheControl wraps an HTTP handler with Cache-Control header
func wrapWithCacheControl(h http.Handler, cacheControl string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", cacheControl)
		h.ServeHTTP(w, r)
	})
}
