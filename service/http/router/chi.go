package router

import (
	"context"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	config "github.com/ponyruntime/pony/api/service/http"
	"net/http"
	"sync"
	"time"
)

// ChiRouter manages router configuration and endpoints
type ChiRouter struct {
	config    config.RouterConfig
	endpoints map[string]config.EndpointConfig
	mu        sync.RWMutex
}

// NewChiRouter creates a new ChiRouter instance
func NewChiRouter(cfg config.RouterConfig) (*ChiRouter, error) {
	return &ChiRouter{
		config:    cfg,
		endpoints: make(map[string]config.EndpointConfig),
	}, nil
}

// AddEndpoint registers a new endpoint configuration
func (rw *ChiRouter) AddEndpoint(endpointID string, cfg config.EndpointConfig) error {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	if _, exists := rw.endpoints[endpointID]; exists {
		return fmt.Errorf("endpoint with ID %s already exists", endpointID)
	}

	rw.endpoints[endpointID] = cfg
	return nil
}

// DeleteEndpoint removes an endpoint configuration by ID
func (rw *ChiRouter) DeleteEndpoint(endpointID string) error {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	if _, exists := rw.endpoints[endpointID]; !exists {
		return fmt.Errorf("endpoint with ID %s not found", endpointID)
	}

	delete(rw.endpoints, endpointID)
	return nil
}

// UpdateEndpoint updates an existing endpoint configuration by ID
func (rw *ChiRouter) UpdateEndpoint(endpointID string, cfg config.EndpointConfig) error {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	if _, exists := rw.endpoints[endpointID]; !exists {
		return fmt.Errorf("endpoint with ID %s not found", endpointID)
	}

	// Check for conflicts with other endpoints
	for id, ec := range rw.endpoints {
		if id != endpointID && ec.Path == cfg.Path && ec.Method == cfg.Method {
			return fmt.Errorf("conflict with existing endpoint: %s %s", cfg.Method, cfg.Path)
		}
	}

	rw.endpoints[endpointID] = cfg
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
	for id, ec := range rw.endpoints {
		wrappedHandler := rw.wrapHandlerWithRouteInfo(handler, ec, id)
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
	endpointID string,
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

		// Create route info with endpoint ID
		routeInfo := &config.RouteInfo{
			Params:     params,
			Endpoint:   endpoint,
			MatchedURI: endpoint.Path,
			EndpointID: endpointID,
		}

		// Add route info to context
		ctx := context.WithValue(r.Context(), config.RouteCtx, routeInfo)

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
func (rw *ChiRouter) GetEndpoints() map[string]config.EndpointConfig {
	rw.mu.RLock()
	defer rw.mu.RUnlock()
	endpoints := make(map[string]config.EndpointConfig, len(rw.endpoints))
	for id, ec := range rw.endpoints {
		endpoints[id] = ec
	}
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

	for id, ec := range rw.endpoints {
		if err := newRouter.AddEndpoint(id, ec); err != nil {
			return nil, fmt.Errorf("failed to add endpoint %s during clone: %w", id, err)
		}
	}

	return newRouter, nil
}
