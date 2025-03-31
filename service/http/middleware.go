package http

import (
	"errors"
	"go.uber.org/zap"
	"net/http"
)

// MiddlewareAPI defines the interface for creating middleware handlers
type MiddlewareAPI interface {
	// CreateMiddleware creates a middleware handler from name and options
	CreateMiddleware(name string, options map[string]string) (func(http.Handler) http.Handler, error)
}

// MiddlewareFactory is the default implementation of MiddlewareAPI
type MiddlewareFactory struct {
	logger        *zap.Logger
	middlewareMap map[string]MiddlewareCreator
}

// MiddlewareCreator is a function that creates a middleware handler from options
type MiddlewareCreator func(options map[string]string) func(http.Handler) http.Handler

// MiddlewareFactoryOption configures a MiddlewareFactory
type MiddlewareFactoryOption func(*MiddlewareFactory)

// WithLogger sets the logger for the middleware factory
func WithLogger(logger *zap.Logger) MiddlewareFactoryOption {
	return func(f *MiddlewareFactory) {
		f.logger = logger
	}
}

// WithMiddleware adds a simple middleware handler to the factory
func WithMiddleware(name string, handler func(http.Handler) http.Handler) MiddlewareFactoryOption {
	return func(f *MiddlewareFactory) {
		f.middlewareMap[name] = func(options map[string]string) func(http.Handler) http.Handler {
			return handler
		}
	}
}

// WithMiddlewareCreator adds a configurable middleware creator to the factory
func WithMiddlewareCreator(name string, creator MiddlewareCreator) MiddlewareFactoryOption {
	return func(f *MiddlewareFactory) {
		f.middlewareMap[name] = creator
	}
}

// NewDefaultMiddlewareFactory creates a new default middleware factory with the provided options
func NewDefaultMiddlewareFactory(options ...MiddlewareFactoryOption) *MiddlewareFactory {
	factory := &MiddlewareFactory{
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
func (f *MiddlewareFactory) CreateMiddleware(name string, options map[string]string) (func(http.Handler) http.Handler, error) {
	if creator, exists := f.middlewareMap[name]; exists {
		handler := creator(options)
		if handler != nil {
			return handler, nil
		}
	}

	return nil, errors.New("middleware not found: " + name)
}
