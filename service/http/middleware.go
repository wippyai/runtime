package http

import (
	"go.uber.org/zap"
	"net/http"
)

// MiddlewareAPI defines the interface for creating middleware handlers
type MiddlewareAPI interface {
	// CreateMiddleware creates a middleware handler from name and options
	CreateMiddleware(name string, options map[string]string) func(http.Handler) http.Handler
}

// DefaultMiddlewareFactory is the default implementation of MiddlewareAPI
type DefaultMiddlewareFactory struct {
	logger        *zap.Logger
	middlewareMap map[string]MiddlewareCreator
}

// MiddlewareCreator is a function that creates a middleware handler from options
type MiddlewareCreator func(options map[string]string) func(http.Handler) http.Handler

// MiddlewareFactoryOption configures a DefaultMiddlewareFactory
type MiddlewareFactoryOption func(*DefaultMiddlewareFactory)

// WithLogger sets the logger for the middleware factory
func WithLogger(logger *zap.Logger) MiddlewareFactoryOption {
	return func(f *DefaultMiddlewareFactory) {
		f.logger = logger
	}
}

// WithMiddleware adds a simple middleware handler to the factory
func WithMiddleware(name string, handler func(http.Handler) http.Handler) MiddlewareFactoryOption {
	return func(f *DefaultMiddlewareFactory) {
		f.middlewareMap[name] = func(options map[string]string) func(http.Handler) http.Handler {
			return handler
		}
	}
}

// WithMiddlewareCreator adds a configurable middleware creator to the factory
func WithMiddlewareCreator(name string, creator MiddlewareCreator) MiddlewareFactoryOption {
	return func(f *DefaultMiddlewareFactory) {
		f.middlewareMap[name] = creator
	}
}

// NewDefaultMiddlewareFactory creates a new default middleware factory with the provided options
func NewDefaultMiddlewareFactory(options ...MiddlewareFactoryOption) *DefaultMiddlewareFactory {
	factory := &DefaultMiddlewareFactory{
		logger:        zap.NewNop(),
		middlewareMap: make(map[string]MiddlewareCreator),
	}

	// Apply options
	for _, option := range options {
		option(factory)
	}

	return factory
}

// CreateMiddleware creates a middleware handler from name and options
func (f *DefaultMiddlewareFactory) CreateMiddleware(name string, options map[string]string) func(http.Handler) http.Handler {
	if creator, exists := f.middlewareMap[name]; exists {
		handler := creator(options)
		if handler != nil {
			return handler
		}
		f.logger.Debug("middleware creator returned nil handler", zap.String("middleware", name))
	}

	f.logger.Debug("middleware not found", zap.String("middleware", name))
	return nil
}
