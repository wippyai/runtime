package http

import (
	"context"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/ponyruntime/pony/api/registry"
	httpapi "github.com/ponyruntime/pony/api/service/http"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// RouteEntry represents a single route within a router
type RouteEntry struct {
	method  string
	path    string
	handler http.Handler
	funcID  registry.ID
}

// RouterEntry represents a router with its routes and middleware
type RouterEntry struct {
	prefix     string
	middleware []func(http.Handler) http.Handler
	routes     map[registry.ID]*RouteEntry
}

// RouteManager handles all routing with full isolation of underlying router implementation
type RouteManager struct {
	routers map[registry.ID]*RouterEntry
	mounts  map[string]http.Handler // Root level mounts
	mu      sync.RWMutex
	router  atomic.Pointer[http.Handler]
}

// NewRouteManager creates a new route manager instance
func NewRouteManager() *RouteManager {
	rm := &RouteManager{
		routers: make(map[registry.ID]*RouterEntry),
		mounts:  make(map[string]http.Handler),
	}
	rm.Build()

	return rm
}

// AddRouter adds a new router or updates an existing one
// If a router with the same Source already exists, it will be updated with the new prefix and middleware
func (rm *RouteManager) AddRouter(id registry.ID, prefix string, middleware []func(http.Handler) http.Handler) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Check if the router already exists
	if existingRouter, exists := rm.routers[id]; exists {
		// Update existing router instead of returning an error
		existingRouter.prefix = prefix
		existingRouter.middleware = middleware
		return nil
	}

	// Create a new router if it doesn't exist
	rm.routers[id] = &RouterEntry{
		prefix:     prefix,
		middleware: middleware,
		routes:     make(map[registry.ID]*RouteEntry),
	}

	return nil
}

// RemoveRouter removes a router by Source
func (rm *RouteManager) RemoveRouter(id registry.ID) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, exists := rm.routers[id]; !exists {
		return fmt.Errorf("router %s not found", id)
	}

	delete(rm.routers, id)
	return nil
}

// AddRoute adds a new route to the specified router
func (rm *RouteManager) AddRoute(routerID registry.ID, id registry.ID, method, path string, funcID registry.ID, handler http.Handler) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	router, exists := rm.routers[routerID]
	if !exists {
		return fmt.Errorf("router %s not found", routerID)
	}

	// Validate method
	method = strings.ToUpper(method)
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodPatch, http.MethodHead, http.MethodOptions, http.MethodTrace:
		// Valid method
	default:
		return fmt.Errorf("invalid HTTP method: %s", method)
	}

	// Validate path
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must start with /: %s", path)
	}

	// Check existing routes
	if _, exists := router.routes[id]; exists {
		return fmt.Errorf("route with Source %s already exists in router %s", id, routerID)
	}

	// Check for conflicts
	for _, route := range router.routes {
		if route.path == path && route.method == method {
			return fmt.Errorf("route with path %s and method %s already exists in router %s", path, method, routerID)
		}
	}

	router.routes[id] = &RouteEntry{
		method:  method,
		path:    path,
		handler: rm.wrapHandler(handler, funcID),
		funcID:  funcID,
	}

	return nil
}

// RemoveRoute removes a route from the specified router
func (rm *RouteManager) RemoveRoute(routerID registry.ID, id registry.ID) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	router, exists := rm.routers[routerID]
	if !exists {
		return fmt.Errorf("router %s not found", routerID)
	}

	if _, exists := router.routes[id]; !exists {
		return fmt.Errorf("route %s not found in router %s", id, routerID)
	}

	delete(router.routes, id)
	return nil
}

// Mount adds a handler at the specified path at the root level
func (rm *RouteManager) Mount(path string, handler http.Handler) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Validate path
	if path == "" {
		return fmt.Errorf("mount path cannot be empty")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("mount path must start with /: %s", path)
	}

	if _, exists := rm.mounts[path]; exists {
		return fmt.Errorf("mount path %s already exists", path)
	}

	rm.mounts[path] = handler
	return nil
}

// Unmount removes a handler from the specified root path
func (rm *RouteManager) Unmount(path string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, exists := rm.mounts[path]; !exists {
		return fmt.Errorf("mount path %s not found", path)
	}

	delete(rm.mounts, path)
	return nil
}

// wrapHandler wraps an HTTP handler with request context information
func (rm *RouteManager) wrapHandler(handler http.Handler, funcID registry.ID) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract URL parameters from chi router context
		rctx := chi.RouteContext(r.Context())
		params := make(map[string]string)

		if rctx != nil {
			for i, key := range rctx.URLParams.Keys {
				if i < len(rctx.URLParams.Values) {
					params[key] = rctx.URLParams.Values[i]
				}
			}
		}

		// Create route info
		routeInfo := &httpapi.RouteInfo{
			Params: params,
			Func:   funcID,
		}

		// Create request context
		reqCtx := httpapi.NewRequestContext(r, w)
		ctx := context.WithValue(r.Context(), httpapi.RequestCtx, reqCtx)
		ctx = context.WithValue(ctx, httpapi.RouteCtx, routeInfo)

		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Build rebuilds the entire router from the current configuration
func (rm *RouteManager) Build() {
	router := chi.NewRouter()

	// Add root mounts first
	for path, handler := range rm.mounts {
		router.Mount(path, handler)
	}

	// Build each router with its middleware and routes
	for _, re := range rm.routers {
		subRouter := chi.NewRouter()

		// Apply router middleware
		for _, mw := range re.middleware {
			subRouter.Use(mw)
		}

		// Add routes to subrouter
		for _, route := range re.routes {
			subRouter.Method(route.method, route.path, route.handler)
		}

		// Mount subrouter
		router.Mount(re.prefix, subRouter)
	}

	var h http.Handler = router
	rm.router.Store(&h)
}

// ServeHTTP implements the http.Handler interface
func (rm *RouteManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	router := rm.router.Load()
	if router == nil {
		http.Error(w, "router not initialized", http.StatusInternalServerError)
		return
	}
	(*router).ServeHTTP(w, r)
}
