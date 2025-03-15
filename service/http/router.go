package http

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/ponyruntime/pony/api/registry"
	httpapi "github.com/ponyruntime/pony/api/service/http"
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
	prefix         string
	middleware     []func(http.Handler) http.Handler
	postMiddleware []func(http.Handler) http.Handler
	routes         map[registry.ID]*RouteEntry
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
func (rm *RouteManager) AddRouter(id registry.ID, prefix string,
	middleware []func(http.Handler) http.Handler,
	postMiddleware []func(http.Handler) http.Handler) error {

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Check if the router already exists
	if existingRouter, exists := rm.routers[id]; exists {
		// Update existing router instead of returning an error
		existingRouter.prefix = prefix
		existingRouter.middleware = middleware
		existingRouter.postMiddleware = postMiddleware
		return nil
	}

	// Create a new router if it doesn't exist
	rm.routers[id] = &RouterEntry{
		prefix:         prefix,
		middleware:     middleware,
		postMiddleware: postMiddleware,
		routes:         make(map[registry.ID]*RouteEntry),
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
		handler: handler, // Store original handler, middleware will be applied at Build time
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

		// Apply router pre-match middleware
		for _, mw := range re.middleware {
			subRouter.Use(mw)
		}

		// Create and add each route with custom handling for post-match middleware
		for routeID, route := range re.routes {
			// Create a custom handler that:
			// 1. Runs first middleware to extract chi params and add route info
			// 2. Runs post-match middleware
			// 3. Finally calls the original handler

			finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Step 1: Extract URL parameters and add route context
				chiRouteCtx := chi.RouteContext(r.Context())
				params := make(map[string]string)

				if chiRouteCtx != nil {
					for i, key := range chiRouteCtx.URLParams.Keys {
						if i < len(chiRouteCtx.URLParams.Values) {
							params[key] = chiRouteCtx.URLParams.Values[i]
						}
					}
				}

				// Create route info
				routeInfo := &httpapi.RouteInfo{
					Params:   params,
					Endpoint: routeID,
					Func:     route.funcID,
				}

				// Create request with updated context
				r = r.WithContext(context.WithValue(r.Context(), httpapi.RouteCtx, routeInfo))

				// Step 2: Run post-match middleware chain
				if len(re.postMiddleware) == 0 {
					// No post middleware, just call the original handler
					route.handler.ServeHTTP(w, r)
					return
				}

				// Create a middleware chain
				chain := createChain(re.postMiddleware, route.handler)
				chain.ServeHTTP(w, r)
			})

			// Register the route with our custom handler
			subRouter.Method(route.method, route.path, finalHandler)
		}

		// Mount subrouter
		router.Mount(re.prefix, subRouter)
	}

	var h http.Handler = router
	rm.router.Store(&h)
}

// createChain creates a middleware chain that will call middlewares in order,
// then call the final handler
func createChain(middleware []func(http.Handler) http.Handler, finalHandler http.Handler) http.Handler {
	// Start with the final handler
	h := finalHandler

	// Apply middleware in reverse order so they execute in the correct order
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}

	return h
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
