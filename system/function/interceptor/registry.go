package interceptor

import (
	"context"
	"sort"
	"sync"

	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/runtime"
	"go.uber.org/zap"
)

type entry struct {
	interceptor function.Interceptor
	order       int
	name        string
}

// Registry manages available interceptors
type Registry struct {
	logger  *zap.Logger
	entries []entry
	chain   *Chain
	mu      sync.RWMutex
}

// NewInterceptorRegistry creates a new interceptor registry
func NewInterceptorRegistry(logger *zap.Logger) *Registry {
	return &Registry{
		logger:  logger,
		entries: make([]entry, 0),
		chain:   nil,
		mu:      sync.RWMutex{},
	}
}

// Register adds an interceptor to the registry
func (r *Registry) Register(name string, interceptor function.Interceptor, order int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range r.entries {
		if e.name == name {
			return NewInterceptorExistsError(name)
		}
	}

	r.entries = append(r.entries, entry{
		interceptor: interceptor,
		order:       order,
		name:        name,
	})

	r.rebuild()

	r.logger.Debug("interceptor registered",
		zap.String("interceptor", name),
		zap.Int("order", order))

	return nil
}

// Unregister removes an interceptor from the registry
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, e := range r.entries {
		if e.name == name {
			r.entries = append(r.entries[:i], r.entries[i+1:]...)
			r.rebuild()

			r.logger.Debug("interceptor unregistered",
				zap.String("interceptor", name))

			return nil
		}
	}

	return NewInterceptorNotFoundError(name)
}

// rebuild recalculates the interceptor chain (must be called with lock held)
func (r *Registry) rebuild() {
	sort.Slice(r.entries, func(i, j int) bool {
		return r.entries[i].order < r.entries[j].order
	})

	interceptors := make([]function.Interceptor, len(r.entries))
	for i, e := range r.entries {
		interceptors[i] = e.interceptor
	}

	chain := newChain(interceptors, r.logger)
	r.chain = &chain
}

// Execute implements the Chain interface using the pre-built chain
func (r *Registry) Execute(ctx context.Context, f function.Func, task runtime.Task) (*runtime.Result, error) {
	r.mu.RLock()
	chain := r.chain
	r.mu.RUnlock()

	if chain == nil {
		return f(ctx, task)
	}

	return chain.Execute(ctx, f, task)
}
