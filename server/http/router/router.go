package router

import (
	"context"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ponyruntime/pony/api/registry"
	config "github.com/ponyruntime/pony/api/server/http"
	"net/http"
	"sync"
)

const (
	DefaultRouterID = "" // Identifier for the default router
)

// GetRouteInfo retrieves route information from the context
func GetRouteInfo(ctx context.Context) (*config.RouteInfo, bool) {
	info, ok := ctx.Value(config.RouteInfoCtx).(*config.RouteInfo)
	return info, ok
}

// Router manages routers, endpoints, and the composed Chi router.
type Router struct {
	handler        http.HandlerFunc                 // The core HTTP handler function
	routers        map[string]*ChiRouter            // Map of router ID to ChiRouter
	endpoints      map[string]config.EndpointConfig // Map of endpoint ID to EndpointConfig
	mu             sync.RWMutex                     // Mutex for concurrency safety
	composedRouter *chi.Mux                         // The composed Chi router
	mur            sync.RWMutex                     // Mutex for concurrency safety of router
}

// NewRouter creates a new Router instance.
func NewRouter(handler http.HandlerFunc) *Router {
	rm := &Router{
		handler:        handler,
		routers:        make(map[string]*ChiRouter),
		endpoints:      make(map[string]config.EndpointConfig),
		composedRouter: chi.NewRouter(),
		mu:             sync.RWMutex{},
		mur:            sync.RWMutex{},
	}

	// Initialize the default router
	rm.routers[DefaultRouterID], _ = NewChiRouter(config.RouterConfig{
		Prefix: "/",
		Meta:   registry.Metadata{"router_id": DefaultRouterID},
	})

	rm.rebuildRouter() // Build the initial composed router

	return rm
}

// AddRouter adds a new router configuration.
func (rm *Router) AddRouter(rcfg config.RouterConfig) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	routerID := rcfg.Meta.StringValue("router_id")
	if routerID == "" {
		return fmt.Errorf("router_id is required in metadata")
	}

	if _, exists := rm.routers[routerID]; exists {
		return fmt.Errorf("router with ID '%s' already exists", routerID)
	}

	newRouter, err := NewChiRouter(rcfg)
	if err != nil {
		return err
	}

	rm.routers[routerID] = newRouter
	rm.rebuildRouter()

	return nil
}

// DeleteRouter deletes a router and its endpoints.
func (rm *Router) DeleteRouter(routerID string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if routerID == DefaultRouterID {
		return fmt.Errorf("cannot delete the default router")
	}

	router, exists := rm.routers[routerID]
	if !exists {
		return fmt.Errorf("router with ID '%s' not found", routerID)
	}

	// Delete all endpoints associated with this router
	for endpointID, ecfg := range rm.endpoints {
		if ecfg.Meta.StringValue("router_id") == routerID {
			delete(rm.endpoints, endpointID)
		}
	}

	// Delete the router itself
	delete(rm.routers, routerID)

	// Remove all routes from the Chi router
	for _, ecfg := range router.GetEndpoints() {
		if err := router.DeleteEndpoint(ecfg.Path, ecfg.Method); err != nil {
			return fmt.Errorf("failed to delete endpoint %s %s from router %s: %w", ecfg.Method, ecfg.Path, routerID, err)
		}
	}

	rm.rebuildRouter()

	return nil
}

// UpdateRouter updates an existing router's configuration.
func (rm *Router) UpdateRouter(rcfg config.RouterConfig) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	routerID := rcfg.Meta.StringValue("router_id")
	if routerID == "" {
		return fmt.Errorf("router_id is required in metadata")
	}

	existingRouter, exists := rm.routers[routerID]
	if !exists {
		return fmt.Errorf("router with ID '%s' not found", routerID)
	}

	// Clone and update the existing router
	newRouter, err := existingRouter.Clone(rcfg)
	if err != nil {
		return err
	}

	rm.routers[routerID] = newRouter
	rm.rebuildRouter()

	return nil
}

// AddEndpoint adds a new endpoint to the appropriate router.
func (rm *Router) AddEndpoint(endpointID string, ecfg config.EndpointConfig) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Generate UUID for endpointID if not provided
	if endpointID == "" {
		endpointID = uuid.NewString()
	}

	routerID := ecfg.Meta.StringValue("router_id")
	if routerID == "" {
		routerID = DefaultRouterID
	}

	router, exists := rm.routers[routerID]
	if !exists {
		return fmt.Errorf("router with ID '%s' not found", routerID)
	}

	// Add endpoint to Chi router
	if err := router.AddEndpoint(ecfg); err != nil {
		return err
	}

	// Store endpoint configuration
	rm.endpoints[endpointID] = ecfg
	rm.rebuildRouter()

	return nil
}

// DeleteEndpoint deletes an endpoint from its router.
func (rm *Router) DeleteEndpoint(endpointID string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	ecfg, exists := rm.endpoints[endpointID]
	if !exists {
		return fmt.Errorf("endpoint with ID '%s' not found", endpointID)
	}

	routerID := ecfg.Meta.StringValue("router_id")
	if routerID == "" {
		routerID = DefaultRouterID
	}

	router, exists := rm.routers[routerID]
	if !exists {
		return fmt.Errorf("router with ID '%s' not found", routerID)
	}

	// Delete endpoint from Chi router
	if err := router.DeleteEndpoint(ecfg.Path, ecfg.Method); err != nil {
		return err
	}

	// Remove endpoint configuration
	delete(rm.endpoints, endpointID)
	rm.rebuildRouter()

	return nil
}

// UpdateEndpoint updates an existing endpoint.
func (rm *Router) UpdateEndpoint(endpointID string, ecfg config.EndpointConfig) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	oldEcfg, exists := rm.endpoints[endpointID]
	if !exists {
		return fmt.Errorf("endpoint with ID '%s' not found", endpointID)
	}

	oldRouterID := oldEcfg.Meta.StringValue("router_id")
	if oldRouterID == "" {
		oldRouterID = DefaultRouterID
	}

	newRouterID := ecfg.Meta.StringValue("router_id")
	if newRouterID == "" {
		newRouterID = DefaultRouterID
	}

	// If router ID changed, delete from old and add to new
	if oldRouterID != newRouterID {
		if err := rm.DeleteEndpoint(endpointID); err != nil {
			return err
		}

		if err := rm.AddEndpoint(endpointID, ecfg); err != nil {
			if err := rm.AddEndpoint(endpointID, oldEcfg); err != nil {
				return fmt.Errorf("failed to rollback endpoint %s to its original configuration: %w", endpointID, err)
			}
			return err
		}

		return nil
	}

	// Update endpoint in the existing router
	router, exists := rm.routers[newRouterID]
	if !exists {
		return fmt.Errorf("router with ID '%s' not found", newRouterID)
	}

	// Add updated endpoint to Chi router
	if err := router.UpdateEndpoint(ecfg); err != nil {
		return err
	}

	// Update endpoint configuration
	rm.endpoints[endpointID] = ecfg
	rm.rebuildRouter()

	return nil
}

// rebuildRouter rebuilds the composed Chi router.
func (rm *Router) rebuildRouter() {
	newRouter := chi.NewRouter()

	for routerID, router := range rm.routers {
		builtRouter, err := router.Build(rm.handler)
		if err != nil {
			// Handle error appropriately (log, panic, etc.)
			fmt.Printf("Error building router %s: %v\n", routerID, err)
			continue
		}
		newRouter.Mount(router.config.Prefix, builtRouter)
	}

	rm.mur.Lock()
	rm.composedRouter = newRouter
	rm.mur.Unlock()
}

// ServeHTTP implements the http.Handler interface for the Router.
func (rm *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rm.mur.RLock()
	defer rm.mur.RUnlock()
	rm.composedRouter.ServeHTTP(w, r)
}
