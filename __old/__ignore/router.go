package __ignore

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	config "github.com/ponyruntime/pony/api/service/http"
)

// todo: can be rewritten or optimized, keep original configs!

// DefaultRouterID is the default router Alias
const DefaultRouterID = ""

// GetRouteInfo retrieves route information from the context
func GetRouteInfo(ctx context.Context) (*config.RouteInfo, bool) {
	info, ok := ctx.Value(config.RouteCtx).(*config.RouteInfo)
	return info, ok
}

type atomicRouter struct {
	value atomic.Pointer[chi.Mux]
}

// Router manages routers, endpoints, and the composed Chi router
type Router struct {
	handler        http.HandlerFunc
	routers        sync.Map     // thread-safe map for routers
	endpoints      sync.Map     // thread-safe map for endpoints
	statics        sync.Map     // thread-safe map for static handlers
	composedRouter atomicRouter // atomic pointer for router updates
}

// NewRouter creates a new Router instance
func NewRouter(handler http.HandlerFunc) *Router {
	rm := &Router{
		handler: handler,
	}

	// Initialize the default router
	defaultRouter, _ := NewChiRouter(config.RouterConfig{Prefix: "/"})
	rm.routers.Store(DefaultRouterID, defaultRouter)

	rm.rebuildRouter() // Build the initial composed router
	return rm
}

// AddRouter adds a new router configuration
func (rm *Router) AddRouter(routerID string, rcfg config.RouterConfig) error {
	newRouter, err := NewChiRouter(rcfg)
	if err != nil {
		return err
	}

	// Atomic store of the new router
	if _, loaded := rm.routers.LoadOrStore(routerID, newRouter); loaded {
		return fmt.Errorf("router with Alias '%s' already exists", routerID)
	}

	rm.rebuildRouter()
	return nil
}

// DeleteRouter deletes a router and its endpoints
func (rm *Router) DeleteRouter(routerID string) error {
	if routerID == DefaultRouterID {
		return fmt.Errorf("cannot delete the default router")
	}

	// Spawn router before deletion
	routerVal, exists := rm.routers.LoadAndDelete(routerID)
	if !exists {
		return fmt.Errorf("router with Alias '%s' not found", routerID)
	}
	router := routerVal.(*ChiRouter)

	// Clean up associated endpoints
	rm.endpoints.Range(func(key, value interface{}) bool {
		ecfg := value.(config.EndpointConfig)
		if ecfg.Meta.StringValue(config.RouterID) == routerID {
			rm.endpoints.Delete(key)
		}
		return true
	})

	// Remove routes from the Chi router
	for id, ecfg := range router.GetEndpoints() {
		if err := router.DeleteEndpoint(id); err != nil {
			return fmt.Errorf("failed to delete endpoint %s %s from router %s: %w",
				ecfg.Method, ecfg.Path, routerID, err)
		}
	}

	rm.rebuildRouter()
	return nil
}

// UpdateRouter updates an existing router's configuration.
func (rm *Router) UpdateRouter(routerID string, rcfg config.RouterConfig) error {
	existingRouter, exists := rm.routers.Load(routerID)
	if !exists {
		return fmt.Errorf("router with Alias '%s' not found", routerID)
	}

	// Clone and update the existing router
	newRouter, err := existingRouter.(*ChiRouter).Clone(rcfg)
	if err != nil {
		return err
	}

	rm.routers.Store(routerID, newRouter)
	rm.rebuildRouter(nil)

	return nil
}

// AddEndpoint adds a new endpoint to the appropriate router.
func (rm *Router) AddEndpoint(endpointID string, cfg config.EndpointConfig) error {
	// Generate UUID for endpointID if not provided
	if endpointID == "" {
		endpointID = uuid.NewString()
	}

	routerID := cfg.Meta.StringValue(config.RouterID)
	if routerID == "" {
		routerID = DefaultRouterID
	}

	router, exists := rm.routers.Load(routerID)
	if !exists {
		return fmt.Errorf("router with Alias '%s' not found", routerID)
	}

	// AddCleanup endpoint to Chi router
	if err := router.(*ChiRouter).AddEndpoint(endpointID, cfg); err != nil {
		return err
	}

	// Store endpoint configuration
	rm.endpoints.Store(endpointID, cfg)
	rm.rebuildRouter(nil)

	return nil
}

// DeleteEndpoint deletes an endpoint from its router.
func (rm *Router) DeleteEndpoint(endpointID string) error {
	ecfg, exists := rm.endpoints.Load(endpointID)
	if !exists {
		return fmt.Errorf("endpoint with Alias '%s' not found", endpointID)
	}

	endpointConfig := ecfg.(config.EndpointConfig)
	routerID := endpointConfig.Meta.StringValue(config.RouterID)
	if routerID == "" {
		routerID = DefaultRouterID
	}

	router, exists := rm.routers.Load(routerID)
	if !exists {
		return fmt.Errorf("router with Alias '%s' not found", routerID)
	}

	// Delete endpoint from Chi router
	if err := router.(*ChiRouter).DeleteEndpoint(endpointID); err != nil {
		return err
	}

	// Remove endpoint configuration
	rm.endpoints.Delete(endpointID)
	rm.rebuildRouter(nil)

	return nil
}

// UpdateEndpoint updates an existing endpoint.
func (rm *Router) UpdateEndpoint(endpointID string, cfg config.EndpointConfig) error {
	oldEcfg, exists := rm.endpoints.Load(endpointID)
	if !exists {
		return fmt.Errorf("endpoint with Alias '%s' not found", endpointID)
	}
	oldEndpointConfig := oldEcfg.(config.EndpointConfig)

	oldRouterID := oldEndpointConfig.Meta.StringValue(config.RouterID)
	if oldRouterID == "" {
		oldRouterID = DefaultRouterID
	}

	newRouterID := cfg.Meta.StringValue(config.RouterID)
	if newRouterID == "" {
		newRouterID = DefaultRouterID
	}

	// If router Alias changed, delete from old and add to new
	if oldRouterID != newRouterID {
		if err := rm.DeleteEndpoint(endpointID); err != nil {
			return err
		}

		if err := rm.AddEndpoint(endpointID, cfg); err != nil {
			if err := rm.AddEndpoint(endpointID, oldEndpointConfig); err != nil {
				return fmt.Errorf("failed to rollback endpoint %s to its original configuration: %w", endpointID, err)
			}
			return err
		}

		return nil
	}

	// Update endpoint in the existing router
	router, exists := rm.routers.Load(newRouterID)
	if !exists {
		return fmt.Errorf("router with Alias '%s' not found", newRouterID)
	}

	// AddCleanup updated endpoint to Chi router
	if err := router.(*ChiRouter).UpdateEndpoint(endpointID, cfg); err != nil {
		return err
	}

	// Update endpoint configuration
	rm.endpoints.Store(endpointID, cfg)

	rm.rebuildRouter()

	return nil
}

// AddStatic adds a new static file server configuration
func (rm *Router) AddStatic(staticID string, cfg config.StaticConfig, fsReg fs.Registry) error {
	// Check if already exists
	if _, loaded := rm.statics.LoadOrStore(staticID, cfg); loaded {
		return fmt.Errorf("static server with ID '%s' already exists", staticID)
	}

	// Verify the filesystem exists before allowing the configuration
	if _, ok := fsReg.GetFS(cfg.FS.String()); !ok {
		rm.statics.Delete(staticID) // Cleanup the stored config
		return fmt.Errorf("filesystem %s not found", cfg.FS)
	}

	rm.rebuildRouter(fsReg)
	return nil
}

// UpdateStatic updates an existing static file server configuration
func (rm *Router) UpdateStatic(staticID string, cfg config.StaticConfig, fsReg fs.Registry) error {
	if _, exists := rm.statics.Load(staticID); !exists {
		return fmt.Errorf("static server with ID '%s' not found", staticID)
	}

	// Verify the filesystem exists before allowing the update
	if _, ok := fsReg.GetFS(cfg.FS.String()); !ok {
		return fmt.Errorf("filesystem %s not found", cfg.FS)
	}

	rm.statics.Store(staticID, cfg)
	rm.rebuildRouter(fsReg)
	return nil
}

// DeleteStatic removes a static file server configuration
func (rm *Router) DeleteStatic(staticID string) error {
	if _, exists := rm.statics.LoadAndDelete(staticID); !exists {
		return fmt.Errorf("static server with ID '%s' not found", staticID)
	}

	rm.rebuildRouter(nil) // nil is safe here as we're just removing
	return nil
}

// rebuildRouter rebuilds the composed Chi router
func (rm *Router) rebuildRouter(fsReg fs.Registry) {
	newRouter := chi.NewRouter()

	// Mount regular routers
	rm.routers.Range(func(key, value interface{}) bool {
		routerID := key.(string)
		router := value.(*ChiRouter)

		builtRouter, err := router.Build(rm.handler)
		if err != nil {
			fmt.Printf("error building router %s: %v\n", routerID, err)
			return true
		}
		newRouter.Mount(router.config.Prefix, builtRouter)
		return true
	})

	// Mount static file servers
	if fsReg != nil {
		rm.statics.Range(func(key, value interface{}) bool {
			cfg := value.(config.StaticConfig)
			fsys, ok := fsReg.GetFS(cfg.FS.String())
			if !ok {
				fmt.Printf("filesystem %s not found\n", cfg.FS)
				return true
			}

			handler := http.FileServer(http.FS(fsys))
			if cfg.Directory != "" {
				handler = http.StripPrefix(cfg.Path, handler)
			}

			if cfg.Options.CacheControl != "" {
				handler = wrapWithCacheControl(handler, cfg.Options.CacheControl)
			}

			newRouter.Mount(cfg.Path, handler)
			return true
		})
	}

	// Atomic router update
	rm.composedRouter.value.Store(newRouter)
}

// ServeHTTP implements the http.ID interface
func (rm *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if router := rm.composedRouter.value.Load(); router != nil {
		router.ServeHTTP(w, r)
	} else {
		http.Error(w, "router not initialized", http.StatusInternalServerError)
	}
}

func wrapWithCacheControl(h http.Handler, cacheControl string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", cacheControl)
		h.ServeHTTP(w, r)
	})
}
