package http

//
//import (
//	"context"
//	"net/http"
//	"sync"
//	"time"
//
//	"github.com/go-chi/chi/v5"
//	"github.com/go-chi/chi/v5/middleware"
//	config "github.com/ponyruntime/pony/api/http"
//	"github.com/ponyruntime/pony/api/tasks"
//	"go.uber.org/zap"
//)
//
//// HttpServer is the main HTTP server.
//type HttpServer struct {
//	log    *zap.Logger
//	config *config.ServerConfig
//	server *http.Server
//
//	routers   map[string]*chi.Mux
//	endpoints []config.Endpoint
//	mu        sync.RWMutex
//
//	executor *tasks.Executor
//}
//
//// NewHttpServer creates a new HttpServer instance.
//func NewHttpServer(log *zap.Logger, config *config.ServerConfig, exec *tasks.Executor) *HttpServer {
//	return &HttpServer{
//		log:      log,
//		config:   config,
//		routers:  make(map[string]*chi.Mux),
//		executor: exec,
//	}
//}
//
//// SetHTTPOptions sets global HTTP options.
//func (s *HttpServer) SetHTTPOptions(httpConfig config.HTTPConfig) {
//	s.mu.Lock()
//	defer s.mu.Unlock()
//
//	s.config.HTTP = httpConfig
//}
//
//// AddRouter adds or updates a router configuration.
//func (s *HttpServer) AddRouter(name string, config config.RouterConfig) {
//	s.mu.Lock()
//	defer s.mu.Unlock()
//
//	router, exists := s.routers[name]
//	if !exists {
//		router = chi.NewRouter()
//		s.routers[name] = router
//	}
//
//	// Apply middleware based on configuration.
//	s.applyMiddleware(router, name, config)
//}
//
//func (s *HttpServer) applyMiddleware(router *chi.Mux, routerName string, config config.RouterConfig) {
//	for _, mwName := range config.Middlewares {
//		switch mwName {
//		case "timeout":
//			timeoutVal := config.Options["timeout"]
//			if timeoutVal == "" {
//				timeoutVal = "60s" // Default timeout
//			}
//			duration, err := time.ParseDuration(timeoutVal)
//			if err != nil {
//				s.log.Error("invalid timeout value", zap.Error(err), zap.String("router", routerName), zap.String("middleware", "timeout"))
//				duration = 60 * time.Second // Default value
//			}
//			router.Use(middleware.Timeout(duration))
//
//		case "recoverer":
//			router.Use(middleware.Recoverer)
//
//		case "request_id":
//			router.Use(middleware.RequestID)
//
//		case "real_ip":
//			router.Use(middleware.RealIP)
//
//		// Add other middleware cases here...
//
//		default:
//			s.log.Warn("unknown middleware", zap.String("name", mwName))
//		}
//	}
//}
//
//// AddEndpoint adds an endpoint configuration.
//func (s *HttpServer) AddEndpoint(endpointConfig config.Endpoint) {
//	s.mu.Lock()
//	defer s.mu.Unlock()
//
//	s.endpoints = append(s.endpoints, endpointConfig)
//	s.createRoute(endpointConfig)
//}
//
//// createRoute encapsulates route creation from Endpoint.
//func (s *HttpServer) createRoute(endpointConfig config.Endpoint) {
//	routerName := endpointConfig.Router
//	if routerName == "" {
//		routerName = "default" // Use default router if not specified
//	}
//
//	router, exists := s.routers[routerName]
//	if !exists {
//		router = chi.NewRouter()
//		s.routers[routerName] = router
//		s.applyRouterConfig(routerName)
//	}
//
//	// Use Executor to load and execute the handler
//	handler, err := s.executor.LoadHandler(endpointConfig.Target)
//	if err != nil {
//		s.log.Error("failed to load handler", zap.Error(err), zap.String("target", endpointConfig.Target))
//		return // Handle the error appropriately
//	}
//
//	router.Method(endpointConfig.Method, endpointConfig.Path, handler)
//}
//
//func (s *HttpServer) applyRouterConfig(routerName string) {
//	router, exists := s.routers[routerName]
//	if !exists {
//		return // Nothing to do if the router doesn't exist
//	}
//
//	// Find the router configuration by name.
//	var routerConfig config.RouterConfig
//	var found bool
//	for _, ep := range s.endpoints {
//		if ep.Router == routerName {
//			for name, config := range s.routers {
//				if name == routerName {
//					routerConfig = config
//					found = true
//					break
//				}
//			}
//		}
//		if found {
//			break
//		}
//	}
//
//	if !found {
//		return // No specific router configuration found
//	}
//
//	// Apply middleware.
//	s.applyMiddleware(router, routerName, routerConfig)
//
//	// Apply prefix if configured.
//	if routerConfig.Prefix != "" {
//		// Wrap the router with a new one that has the prefix.
//		prefixRouter := chi.NewRouter()
//		prefixRouter.Mount(routerConfig.Prefix, router)
//
//		// Replace the original router with the prefixed one.
//		s.routers[routerName] = prefixRouter
//	}
//}
//
//// Serve starts the HTTP server.
//func (s *HttpServer) Serve(ctx context.Context) error {
//	s.mu.Lock()
//	s.server = &http.Server{
//		Addr:         s.config.Addr,
//		Handler:      s.buildRouter(),
//		ReadTimeout:  s.config.HTTP.ReadTimeout,
//		WriteTimeout: s.config.HTTP.WriteTimeout,
//		IdleTimeout:  s.config.HTTP.IdleTimeout,
//	}
//	s.mu.Unlock()
//
//	s.log.Info("starting server", zap.String("addr", s.config.Addr))
//
//	// Start the server in a goroutine
//	go func() {
//		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
//			s.log.Error("server error", zap.Error(err))
//		}
//	}()
//
//	// Wait for either context cancellation or server shutdown
//	select {
//	case <-ctx.Done():
//		// Context was canceled, shut down the server gracefully
//		s.log.Info("context canceled, shutting down server", zap.Error(ctx.Err()))
//		return s.Stop(ctx)
//	}
//}
//
//// Stop gracefully stops the HTTP server.
//func (s *HttpServer) Stop(ctx context.Context) error {
//	s.log.Info("stopping server")
//	if s.server != nil {
//		return s.server.Shutdown(ctx)
//	}
//	return nil
//}
//
//func (s *HttpServer) buildRouter() http.Handler {
//	s.mu.RLock()
//	defer s.mu.RUnlock()
//
//	mainRouter := chi.NewRouter()
//
//	// Mount all subrouters (with prefixes) onto the main router.
//	for name, r := range s.routers {
//		if routerConfig, ok := s.getRouterConfig(name); ok {
//			mainRouter.Mount(routerConfig.Prefix, r)
//		} else {
//			mainRouter.Mount("/", r) // Mount without prefix if config not found
//		}
//	}
//
//	return mainRouter
//}
//
//func (s *HttpServer) getRouterConfig(name string) (config.RouterConfig, bool) {
//	// Iterate over the endpoints and find the router configuration
//	for _, ep := range s.endpoints {
//		if ep.Router == name {
//			for routerName, config := range s.routers {
//				if routerName == name {
//					return config, true
//				}
//			}
//		}
//	}
//
//	return config.RouterConfig{}, false
//}
