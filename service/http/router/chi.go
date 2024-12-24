package router

import (
	"context"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	config "github.com/ponyruntime/pony/api/server/http"
	"net/http"
	"sync"
	"time"
)

// ChiRouter manages router configuration and endpoints
type ChiRouter struct {
	config    config.RouterConfig
	endpoints []config.EndpointConfig
	mu        sync.RWMutex
}

// NewChiRouter creates a new ChiRouter instance
func NewChiRouter(cfg config.RouterConfig) (*ChiRouter, error) {
	return &ChiRouter{
		config:    cfg,
		endpoints: []config.EndpointConfig{},
	}, nil
}

// AddEndpoint registers a new endpoint configuration
func (rw *ChiRouter) AddEndpoint(ecfg config.EndpointConfig) error {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	for _, ec := range rw.endpoints {
		if ec.Path == ecfg.Path && ec.Method == ecfg.Method {
			return fmt.Errorf("duplicate endpoint: %s %s", ecfg.Method, ecfg.Path)
		}
	}

	rw.endpoints = append(rw.endpoints, ecfg)
	return nil
}

// DeleteEndpoint removes an endpoint configuration
func (rw *ChiRouter) DeleteEndpoint(path, method string) error {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	newEndpoints := make([]config.EndpointConfig, 0, len(rw.endpoints))
	found := false
	for _, ec := range rw.endpoints {
		if ec.Path == path && ec.Method == method {
			found = true
			continue
		}
		newEndpoints = append(newEndpoints, ec)
	}

	if !found {
		return fmt.Errorf("endpoint not found: %s %s", method, path)
	}

	rw.endpoints = newEndpoints
	return nil
}

// UpdateEndpoint updates an existing endpoint configuration
func (rw *ChiRouter) UpdateEndpoint(ecfg config.EndpointConfig) error {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	found := false
	for i, ec := range rw.endpoints {
		if ec.Path == ecfg.Path && ec.Method == ecfg.Method {
			rw.endpoints[i] = ecfg
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("endpoint not found for update: %s %s", ecfg.Method, ecfg.Path)
	}

	return nil
}

// Build creates and returns a new Chi router instance with all configured endpoints
func (rw *ChiRouter) Build(handler http.HandlerFunc) (*chi.Mux, error) {
	rw.mu.RLock()
	defer rw.mu.RUnlock()

	router := chi.NewRouter()

	// Set default handlers
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintln(w, "404 page not found")
	})

	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprintln(w, "405 method not allowed")
	})

	// Apply middleware
	for _, mw := range rw.makeMiddleware(rw.config) {
		router.Use(mw)
	}

	// Register all endpoints with wrapped handlers
	for _, ec := range rw.endpoints {
		wrappedHandler := rw.wrapHandlerWithRouteInfo(handler, ec, ec.Path)
		router.Method(ec.Method, ec.Path, wrappedHandler)
	}

	return router, nil
}

// makeMiddleware returns a list of middleware handlers based on the provided configuration
func (rw *ChiRouter) makeMiddleware(opts config.RouterConfig) []func(http.Handler) http.Handler {
	var middlewares []func(http.Handler) http.Handler

	for _, mwName := range opts.Middlewares {
		switch mwName {
		case "timeout":
			timeoutVal := opts.Options["timeout"]
			if timeoutVal == "" {
				timeoutVal = "60s" // Default timeout
			}
			duration, err := time.ParseDuration(timeoutVal)
			if err != nil {
				continue
			}
			middlewares = append(middlewares, middleware.Timeout(duration))
		case "recoverer":
			middlewares = append(middlewares, middleware.Recoverer)
		case "request_id":
			middlewares = append(middlewares, middleware.RequestID)
		case "real_ip":
			middlewares = append(middlewares, middleware.RealIP)
		}
	}

	return middlewares
}

// wrapHandlerWithRouteInfo creates a handler that injects route information into the context
func (rw *ChiRouter) wrapHandlerWithRouteInfo(
	handler http.HandlerFunc,
	endpoint config.EndpointConfig,
	fullPath string,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract URL parameters
		params := make(map[string]string)
		rctx := chi.RouteContext(r.Context())
		if rctx != nil {
			for i, key := range rctx.URLParams.Keys {
				if i < len(rctx.URLParams.Values) {
					params[key] = rctx.URLParams.Values[i]
				}
			}
		}

		// Create route info
		routeInfo := &config.RouteInfo{
			Params:     params,
			Endpoint:   endpoint,
			MatchedURI: fullPath,
		}

		// Add route info to context
		ctx := context.WithValue(r.Context(), config.RouteInfoCtx, routeInfo)

		// Call handler with enhanced context
		handler(w, r.WithContext(ctx))
	}
}

// GetConfig retrieves the current configuration
func (rw *ChiRouter) GetConfig() config.RouterConfig {
	rw.mu.RLock()
	defer rw.mu.RUnlock()
	return rw.config
}

// GetEndpoints retrieves a copy of the current endpoint configurations
func (rw *ChiRouter) GetEndpoints() []config.EndpointConfig {
	rw.mu.RLock()
	defer rw.mu.RUnlock()
	endpoints := make([]config.EndpointConfig, len(rw.endpoints))
	copy(endpoints, rw.endpoints)
	return endpoints
}

// Clone creates a copy of the ChiRouter with a new configuration
func (rw *ChiRouter) Clone(ncfg config.RouterConfig) (*ChiRouter, error) {
	rw.mu.RLock()
	defer rw.mu.RUnlock()

	newRouter, err := NewChiRouter(ncfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create new router: %w", err)
	}

	for _, ec := range rw.endpoints {
		if err := newRouter.AddEndpoint(ec); err != nil {
			return nil, fmt.Errorf("failed to add endpoint during clone: %w", err)
		}
	}

	return newRouter, nil
}
