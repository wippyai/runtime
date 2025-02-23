package http

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/fs"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	config "github.com/ponyruntime/pony/api/service/http"
	"net/http"
)

// EndpointFactory creates HTTP handlers for function endpoints
type EndpointFactory struct {
	funcs function.Registry
	dtt   payload.Transcoder
}

// Ensure EndpointFactory implements EndpointFactoryAPI
var _ EndpointFactoryAPI = (*EndpointFactory)(nil)

func NewEndpointFactory(funcs function.Registry) (*EndpointFactory, error) {
	if funcs == nil {
		return nil, fmt.Errorf("function registry is required")
	}
	return &EndpointFactory{
		funcs: funcs,
	}, nil
}

func (f *EndpointFactory) CreateHandler(ctx context.Context, cfg *config.EndpointConfig) (http.Handler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid endpoint config: %w", err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rCtx := config.NewRequestContext(r, w)
		execCtx := context.WithValue(r.Context(), config.RequestCtx, rCtx)

		resultCh, err := f.funcs.Call(execCtx, runtime.Task{ID: cfg.Func})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		select {
		case result := <-resultCh:
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
			http.Error(w, "request canceled", http.StatusInternalServerError)
			return
		}
	}), nil
}

// StaticFactory creates HTTP handlers for static file serving
type StaticFactory struct {
	fsReg fs.Registry
}

// Ensure StaticFactory implements StaticFactoryAPI
var _ StaticFactoryAPI = (*StaticFactory)(nil)

func NewStaticFactory(fsReg fs.Registry) (*StaticFactory, error) {
	if fsReg == nil {
		return nil, fmt.Errorf("filesystem registry is required")
	}
	return &StaticFactory{
		fsReg: fsReg,
	}, nil
}

func (f *StaticFactory) CreateHandler(ctx context.Context, cfg *config.StaticConfig) (http.Handler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid static config: %w", err)
	}

	fsys, ok := f.fsReg.GetFS(cfg.FS.String())
	if !ok {
		return nil, fmt.Errorf("filesystem not found: %s", cfg.FS)
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

type ServerFactory struct{}

func NewServerFactory() *ServerFactory {
	return &ServerFactory{}
}

func (f *ServerFactory) CreateServer(cfg *config.ServerConfig) (Server, error) {
	return NewServerService(cfg), nil
}

func wrapWithCacheControl(h http.Handler, cacheControl string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", cacheControl)
		h.ServeHTTP(w, r)
	})
}
