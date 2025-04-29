package http

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/ponyruntime/pony/api/fs"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	config "github.com/ponyruntime/pony/api/service/http"
)

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
		rCtx := config.NewRequestContext(r, w)
		execCtx := context.WithValue(r.Context(), config.RequestCtx, rCtx)

		resultCh, err := f.funcs.Call(execCtx, runtime.Task{ID: cfg.Func})
		if err != nil {
			if !rCtx.ResponseHandled() {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		select {
		case result := <-resultCh:
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
			if !rCtx.ResponseHandled() {
				http.Error(w, "no response sent", http.StatusInternalServerError)
			}
		case <-r.Context().Done():
			if rCtx.ResponseHandled() {
				return
			}
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
	fsReg fs.Registry
}

// Ensure StaticFactory implements StaticFactoryAPI
var _ StaticFactoryAPI = (*StaticFactory)(nil)

// NewStaticFactory creates a new static file factory instance with the provided filesystem registry
func NewStaticFactory(fsReg fs.Registry) (*StaticFactory, error) {
	if fsReg == nil {
		return nil, fmt.Errorf("filesystem registry is required")
	}
	return &StaticFactory{
		fsReg: fsReg,
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

	// For SPA mode, use our custom handler
	if cfg.Options.SPA {
		if cfg.Options.IndexFile == "" {
			return nil, fmt.Errorf("index file must be specified for SPA mode")
		}
		handler := NewSPAHandler(fsys, cfg.Options.IndexFile)

		if cfg.Options.CacheControl != "" {
			handler = wrapWithCacheControl(handler, cfg.Options.CacheControl)
		}

		return handler, nil
	}

	handler := http.FileServer(http.FS(fsys))

	if cfg.Directory != "" {
		handler = http.StripPrefix(cfg.Path, handler)
	}

	if cfg.Options.CacheControl != "" {
		handler = wrapWithCacheControl(handler, cfg.Options.CacheControl)
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
	return NewServerService(id, cfg, f.middlewareFactory), nil
}

// wrapWithCacheControl wraps an HTTP handler with Cache-Control header
func wrapWithCacheControl(h http.Handler, cacheControl string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", cacheControl)
		h.ServeHTTP(w, r)
	})
}
