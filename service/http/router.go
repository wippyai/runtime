package http

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	httpapi "github.com/wippyai/runtime/api/service/http"
)

// RouteInfoPool safely pools RouteInfo objects to reduce allocations
var routeInfoPool = sync.Pool{
	New: func() interface{} {
		return &httpapi.RouteInfo{
			Params: make(map[string]string, 2),
		}
	},
}

// getRouteInfo gets a RouteInfo from pool and resets it
func getRouteInfo() *httpapi.RouteInfo {
	info := routeInfoPool.Get().(*httpapi.RouteInfo)

	// Clear the map but keep the underlying storage
	for k := range info.Params {
		delete(info.Params, k)
	}

	// Reset other fields
	info.Endpoint = registry.ID{}
	info.Func = registry.ID{}

	return info
}

// putRouteInfo returns RouteInfo to pool
func putRouteInfo(info *httpapi.RouteInfo) {
	if info != nil {
		routeInfoPool.Put(info)
	}
}

// RouteEntry represents a single route within a router
type RouteEntry struct {
	method     string
	path       string
	handler    http.Handler
	funcID     registry.ID
	paramNames []string // Cached param names extracted from path
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
func NewRouteManager() (*RouteManager, error) {
	rm := &RouteManager{
		routers: make(map[registry.ID]*RouterEntry),
		mounts:  make(map[string]http.Handler),
	}
	err := rm.Build()
	if err != nil {
		return nil, err
	}

	return rm, nil
}

// AddRouter adds a new router or updates an existing one
func (rm *RouteManager) AddRouter(id registry.ID, prefix string,
	middleware []func(http.Handler) http.Handler,
	postMiddleware []func(http.Handler) http.Handler) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Check for duplicate prefixes
	for existingID, existingRouter := range rm.routers {
		if existingID != id && existingRouter.prefix == prefix {
			return fmt.Errorf("router with prefix %s already exists", prefix)
		}
	}

	// Check if the router already exists
	if existingRouter, exists := rm.routers[id]; exists {
		// Update existing router
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

// RemoveRouter removes a router by ID
func (rm *RouteManager) RemoveRouter(id registry.ID) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, exists := rm.routers[id]; !exists {
		return fmt.Errorf("router %s not found", id)
	}

	delete(rm.routers, id)
	return nil
}

// AddRoute adds or updates a route in the specified router
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

	// Convert :param syntax to {param} syntax
	pathWithBraceSyntax := path
	if strings.Contains(path, ":") {
		segments := strings.Split(path, "/")
		for i, segment := range segments {
			if strings.HasPrefix(segment, ":") {
				paramName := segment[1:]
				segments[i] = "{" + paramName + "}"
			}
		}
		pathWithBraceSyntax = strings.Join(segments, "/")
	}

	// Extract param names for later use
	paramNames := extractParamNames(pathWithBraceSyntax)

	// Upsert route (allow overwrites for updates)
	router.routes[id] = &RouteEntry{
		method:     method,
		path:       pathWithBraceSyntax,
		handler:    handler,
		funcID:     funcID,
		paramNames: paramNames,
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
func (rm *RouteManager) Build() error {
	mux := http.NewServeMux()

	// Add root mounts
	for path, handler := range rm.mounts {
		pattern := path
		if !strings.HasSuffix(pattern, "/") {
			pattern += "/"
		}
		mux.Handle(pattern, handler)
	}

	// Build routes from all routers
	registeredOptions := make(map[string]bool)

	for _, routerEntry := range rm.routers {
		for routeID, route := range routerEntry.routes {
			// Build full pattern: "METHOD /prefix/path"
			pattern := buildPattern(route.method, routerEntry.prefix, route.path)

			// Create handler that extracts params and applies post-middleware
			handler := rm.createRouteHandler(routeID, route, routerEntry)

			// Apply pre-match middleware (CORS, RealIP, etc.)
			if len(routerEntry.middleware) > 0 {
				handler = applyMiddlewareChain(routerEntry.middleware, handler)
			}

			mux.Handle(pattern, handler)

			// Generate OPTIONS handler for CORS preflight (once per path)
			if route.method != "OPTIONS" {
				optionsPattern := buildPattern("OPTIONS", routerEntry.prefix, route.path)

				// Only register if not already registered
				if !registeredOptions[optionsPattern] {
					optionsHandler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.WriteHeader(http.StatusNoContent)
					}))

					// Apply same pre-match middleware to OPTIONS
					if len(routerEntry.middleware) > 0 {
						optionsHandler = applyMiddlewareChain(routerEntry.middleware, optionsHandler)
					}

					mux.Handle(optionsPattern, optionsHandler)
					registeredOptions[optionsPattern] = true
				}
			}
		}
	}

	var h http.Handler = mux
	rm.router.Store(&h)

	return nil
}

// createRouteHandler creates the handler for a route with param extraction and post-middleware
func (rm *RouteManager) createRouteHandler(routeID registry.ID, route *RouteEntry, routerEntry *RouterEntry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get pooled RouteInfo
		routeInfo := getRouteInfo()
		defer putRouteInfo(routeInfo)

		// Extract URL parameters using req.PathValue()
		for _, paramName := range route.paramNames {
			value := r.PathValue(paramName)
			if value != "" {
				routeInfo.Params[paramName] = value
			}
		}

		// Set route metadata
		routeInfo.Endpoint = routeID
		routeInfo.Func = route.funcID

		// Add route info to FrameContext
		fc := contextapi.FrameFromContext(r.Context())
		if fc != nil {
			_ = fc.Set(httpapi.RouteCtx, routeInfo)
		}

		// Apply post-match middleware and call endpoint handler
		finalHandler := route.handler
		if len(routerEntry.postMiddleware) > 0 {
			finalHandler = applyMiddlewareChain(routerEntry.postMiddleware, finalHandler)
		}
		finalHandler.ServeHTTP(w, r)
	})
}

// buildPattern builds the full pattern for ServeMux
func buildPattern(method, prefix, path string) string {
	// Normalize prefix (remove trailing slash)
	prefix = strings.TrimSuffix(prefix, "/")

	// Build full path
	fullPath := prefix + path

	return method + " " + fullPath
}

// extractParamNames extracts parameter names from path like "/users/{id}/posts/{postId}" -> ["id", "postId"]
func extractParamNames(path string) []string {
	var names []string
	start := -1

	for i, ch := range path {
		if ch == '{' {
			start = i + 1
		} else if ch == '}' && start >= 0 {
			names = append(names, path[start:i])
			start = -1
		}
	}

	return names
}

// applyMiddlewareChain wraps handler with middleware in correct order
func applyMiddlewareChain(middleware []func(http.Handler) http.Handler, finalHandler http.Handler) http.Handler {
	h := finalHandler
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
