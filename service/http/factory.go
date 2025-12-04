package http

import (
	"context"
	"net/http"
	"path"
	"strings"

	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	config "github.com/wippyai/runtime/api/service/http"
)

// newRequestContext creates a RequestContext for the request.
// Note: We don't pool RequestContext because the Lua worker may still be
// accessing it after the HTTP handler returns (e.g., when request is cancelled
// but the pool worker is still executing). Pooling would cause races.
func newRequestContext(r *http.Request, w http.ResponseWriter) *config.RequestContext {
	ctx := &config.RequestContext{}
	ctx.SetRequest(r)
	ctx.SetResponseWriter(w)
	ctx.ResetHandled()
	return ctx
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
		return nil, ErrFunctionRegistryRequired
	}
	return &EndpointFactory{
		funcs: funcs,
	}, nil
}

// CreateHandler creates an HTTP handler from the provided endpoint configuration
func (f *EndpointFactory) CreateHandler(_ context.Context, cfg *config.EndpointConfig) (http.Handler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, NewInvalidEndpointConfigError(err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create RequestContext for this request
		rCtx := newRequestContext(r, w)

		// Use existing context (FrameContext already created in handler wrapper)
		execCtx := r.Context()

		// Seal the frame context before calling the function
		// This allows middleware to set values (actor, etc.) in unsealed frame
		// while preventing request context from leaking to child functions
		fc := contextapi.FrameFromContext(execCtx)
		if fc != nil {
			fc.Seal()
		}

		// Create task with request context as pairs (not in frame)
		// This prevents request context from leaking to child function calls
		// Note: We don't pre-read the body here - the Lua function can access it
		// via req:body() or req:stream() as needed
		task := runtime.Task{
			ID: cfg.Func,
			Context: []contextapi.Pair{
				{Key: config.RequestCtxKey(), Value: rCtx},
			},
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
		return nil, ErrFilesystemRegistryRequired
	}
	if middlewareFactory == nil {
		return nil, ErrMiddlewareFactoryRequired
	}
	return &StaticFactory{
		fsReg:             fsReg,
		middlewareFactory: middlewareFactory,
	}, nil
}

// CreateHandler creates an HTTP handler from the provided static file configuration
func (f *StaticFactory) CreateHandler(_ context.Context, cfg *config.StaticConfig) (http.Handler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, NewInvalidStaticConfigError(err)
	}

	fsys, ok := f.fsReg.GetFS(cfg.FS.String())
	if !ok {
		return nil, NewFilesystemNotFoundError(cfg.FS.String())
	}

	// Create base handler
	var handler http.Handler

	// For SPA mode, use our custom handler
	if cfg.StaticOptions.SPA {
		if cfg.StaticOptions.IndexFile == "" {
			return nil, ErrIndexFileRequired
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
				return nil, NewMiddlewareCreateError(name, err)
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
