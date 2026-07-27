// SPDX-License-Identifier: MPL-2.0

package http

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/registry"
	httpapi "github.com/wippyai/runtime/api/service/http"
)

// RouteInfoPool safely pools RouteInfo objects to reduce allocations
var routeInfoPool = sync.Pool{
	New: func() any {
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
	info.Endpoint = registry.NewID("", "")
	info.Func = registry.NewID("", "")

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
	routes         map[registry.ID]*RouteEntry
	prefix         string
	middleware     []func(http.Handler) http.Handler
	postMiddleware []func(http.Handler) http.Handler
}

// RouteManager handles all routing with full isolation of underlying router implementation
type RouteManager struct {
	routers map[registry.ID]*RouterEntry
	mounts  map[string]http.Handler
	router  atomic.Pointer[http.Handler]
	mu      sync.RWMutex
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
			return NewRouterPrefixExistsError(prefix)
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
		return NewRouterNotFoundError(id.String())
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
		return NewRouterNotFoundError(routerID.String())
	}

	// Validate method
	method = strings.ToUpper(method)
	switch method {
	case httpapi.MethodAny, http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodPatch, http.MethodHead, http.MethodOptions, http.MethodTrace:
		// Valid method
	default:
		return httpapi.NewInvalidHTTPMethodError(method)
	}
	if method == httpapi.MethodAny {
		method = ""
	}

	// Validate path
	if path == "" {
		return ErrPathCannotBeEmpty
	}
	if !strings.HasPrefix(path, "/") {
		return NewInvalidPathError(path)
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
		return NewRouterNotFoundError(routerID.String())
	}

	if _, exists := router.routes[id]; !exists {
		return NewRouteNotFoundError(id.String(), routerID.String())
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
		return ErrMountPathCannotBeEmpty
	}
	if !strings.HasPrefix(path, "/") {
		return NewInvalidMountPathError(path)
	}

	if _, exists := rm.mounts[path]; exists {
		return NewMountPathExistsError(path)
	}

	rm.mounts[path] = handler
	return nil
}

// ReplaceMount replaces an existing root mount in place.
func (rm *RouteManager) ReplaceMount(path string, handler http.Handler) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if path == "" {
		return ErrMountPathCannotBeEmpty
	}
	if !strings.HasPrefix(path, "/") {
		return NewInvalidMountPathError(path)
	}
	if _, exists := rm.mounts[path]; !exists {
		return NewMountPathNotFoundError(path)
	}

	rm.mounts[path] = handler
	return nil
}

// Unmount removes a handler from the specified root path
func (rm *RouteManager) Unmount(path string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, exists := rm.mounts[path]; !exists {
		return NewMountPathNotFoundError(path)
	}

	delete(rm.mounts, path)
	return nil
}

// Build rebuilds the entire router from the current configuration
func (rm *RouteManager) Build() error {
	// Collect all patterns first to check for conflicts
	type patternEntry struct {
		handler http.Handler
		pattern string
	}
	var allPatterns []patternEntry

	// Add root mounts
	for path, handler := range rm.mounts {
		pattern := path
		if !strings.HasSuffix(pattern, "/") {
			pattern += "/"
		}
		allPatterns = append(allPatterns, patternEntry{handler, pattern})
	}

	// Collect route patterns
	registeredOptions := make(map[string]bool)
	for _, routerEntry := range rm.routers {
		for routeID, route := range routerEntry.routes {
			pattern := buildPattern(route.method, routerEntry.prefix, route.path)
			handler := rm.createRouteHandler(routeID, route, routerEntry)
			if len(routerEntry.middleware) > 0 {
				handler = applyMiddlewareChain(routerEntry.middleware, handler)
			}
			// Set the route label as the outermost wrapper so pre-match middleware
			// (e.g., OTel, http metrics) observes the real route instead of "unmatched".
			handler = withRouteLabel(routeLabelFor(route.funcID, routeID), handler)
			allPatterns = append(allPatterns, patternEntry{handler, pattern})

			// Auto-generate OPTIONS handler so CORS middleware can intercept preflight
			if route.method != "" && route.method != http.MethodOptions {
				optionsPattern := buildPattern("OPTIONS", routerEntry.prefix, route.path)
				if !registeredOptions[optionsPattern] {
					optionsHandler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.WriteHeader(http.StatusNoContent)
					}))
					if len(routerEntry.middleware) > 0 {
						optionsHandler = applyMiddlewareChain(routerEntry.middleware, optionsHandler)
					}
					optionsHandler = withRouteLabel(routeLabelFor(route.funcID, routeID), optionsHandler)
					allPatterns = append(allPatterns, patternEntry{optionsHandler, optionsPattern})
					registeredOptions[optionsPattern] = true
				}
			}
		}
	}

	// Let ServeMux apply its complete method, path, wildcard, and subtree
	// precedence rules. Convert registration panics into configuration errors so
	// an invalid route set cannot crash the process during a rebuild.
	mux := http.NewServeMux()
	for _, p := range allPatterns {
		if err := registerPattern(mux, p.pattern, p.handler); err != nil {
			return err
		}
	}

	var h http.Handler = mux
	rm.router.Store(&h)

	return nil
}

func registerPattern(mux *http.ServeMux, pattern string, handler http.Handler) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = NewRouteConflictsError([]string{fmt.Sprint(recovered)})
		}
	}()

	mux.Handle(pattern, handler)
	return nil
}

// routeLabelFor returns the route label used for observability, preferring the
// function ID and falling back to the route ID.
func routeLabelFor(funcID, routeID registry.ID) string {
	if l := funcID.String(); l != "" {
		return l
	}
	return routeID.String()
}

// withRouteLabel wraps next so the route label is set in the request frame
// before next runs. Applied as the outermost route wrapper so pre-match
// middleware (OTel, http metrics) sees the real route instead of "unmatched".
func withRouteLabel(label string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = httpapi.SetRouteLabel(r.Context(), label)
		next.ServeHTTP(w, r)
	})
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

		// Add route info to FrameContext. The route label is set by the outer
		// withRouteLabel wrapper so pre-match middleware (OTel, http metrics)
		// already observes it; SetRouteInfo is still needed for downstream handlers.
		_ = httpapi.SetRouteInfo(r.Context(), routeInfo)

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
	if method == "" {
		return fullPath
	}

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
			name := path[start:i]
			names = append(names, strings.TrimSuffix(name, "..."))
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
