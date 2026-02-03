package http

import (
	"errors"
	"net/http"
	"sync"

	"go.uber.org/zap"
)

// MiddlewareAPI defines the interface for creating middleware handlers
type MiddlewareAPI interface {
	// CreateMiddleware creates a middleware handler from name and options
	CreateMiddleware(name string, options map[string]string) (func(http.Handler) http.Handler, error)
	// Register adds a middleware creator to the registry
	Register(name string, creator MiddlewareCreator) error
	// Unregister removes a middleware creator from the registry
	Unregister(name string) error
}

// MiddlewareRegistry is the implementation of MiddlewareAPI with registration support
type MiddlewareRegistry struct {
	logger        *zap.Logger
	middlewareMap map[string]MiddlewareCreator
	mu            sync.RWMutex
}

// MiddlewareCreator is a function that creates a middleware handler from options
type MiddlewareCreator = func(options map[string]string) func(http.Handler) http.Handler

// NewMiddlewareRegistry creates a new middleware registry
func NewMiddlewareRegistry(logger *zap.Logger) *MiddlewareRegistry {
	return &MiddlewareRegistry{
		logger:        logger,
		middlewareMap: make(map[string]MiddlewareCreator),
		mu:            sync.RWMutex{},
	}
}

// Register adds a middleware creator to the registry
func (r *MiddlewareRegistry) Register(name string, creator MiddlewareCreator) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.middlewareMap[name]; exists {
		return errors.New("middleware already registered: " + name)
	}

	r.middlewareMap[name] = creator
	r.logger.Debug("middleware registered", zap.String("name", name))

	return nil
}

// Unregister removes a middleware creator from the registry
func (r *MiddlewareRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.middlewareMap[name]; !exists {
		return errors.New("middleware not found: " + name)
	}

	delete(r.middlewareMap, name)
	r.logger.Debug("middleware unregistered", zap.String("name", name))

	return nil
}

// CreateMiddleware creates a middleware handler from name and options
func (r *MiddlewareRegistry) CreateMiddleware(name string, options map[string]string) (func(http.Handler) http.Handler, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if creator, exists := r.middlewareMap[name]; exists {
		handler := creator(options)
		if handler != nil {
			r.logger.Debug("middleware created", zap.String("name", name))
			return handler, nil
		}
		r.logger.Warn("middleware creator returned nil", zap.String("name", name))
	}

	return nil, errors.New("middleware not found: " + name)
}
