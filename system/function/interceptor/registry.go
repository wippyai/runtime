package interceptor

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/runtime"
	"go.uber.org/zap"
)

type entry struct {
	interceptor function.Interceptor
	order       int
	name        string
}

// sealedChain holds the immutable interceptor slice after sealing
type sealedChain struct {
	interceptors []function.Interceptor
}

// InterceptorRegistry manages available interceptors.
// Uses atomic pointer for lock-free Execute() on hot path.
type InterceptorRegistry struct {
	logger  *zap.Logger
	entries []entry
	mu      sync.Mutex
	chain   atomic.Pointer[sealedChain]
}

// NewInterceptorRegistry creates a new interceptor registry.
func NewInterceptorRegistry(logger *zap.Logger) *InterceptorRegistry {
	return &InterceptorRegistry{
		logger:  logger,
		entries: make([]entry, 0),
	}
}

// Register adds an interceptor to the registry.
func (r *InterceptorRegistry) Register(name string, interceptor function.Interceptor, order int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range r.entries {
		if e.name == name {
			return function.NewInterceptorExistsError(name)
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

// Unregister removes an interceptor from the registry.
func (r *InterceptorRegistry) Unregister(name string) error {
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

	return function.NewInterceptorNotFoundError(name)
}

// rebuild creates the sealed chain (called with lock held)
func (r *InterceptorRegistry) rebuild() {
	sort.Slice(r.entries, func(i, j int) bool {
		return r.entries[i].order < r.entries[j].order
	})

	if len(r.entries) == 0 {
		r.chain.Store(nil)
		return
	}

	interceptors := make([]function.Interceptor, len(r.entries))
	for i, e := range r.entries {
		interceptors[i] = e.interceptor
	}

	r.chain.Store(&sealedChain{interceptors: interceptors})
}

// Execute runs the interceptor chain. Lock-free on hot path.
func (r *InterceptorRegistry) Execute(ctx context.Context, f function.Func, task runtime.Task) (*runtime.Result, error) {
	chain := r.chain.Load()
	if chain == nil || len(chain.interceptors) == 0 {
		return f(ctx, task)
	}

	return r.executeAt(ctx, f, task, chain.interceptors, 0)
}

// executeAt runs interceptor at index i
func (r *InterceptorRegistry) executeAt(ctx context.Context, f function.Func, task runtime.Task, interceptors []function.Interceptor, i int) (*runtime.Result, error) {
	if i >= len(interceptors) {
		return f(ctx, task)
	}

	return interceptors[i].Handle(ctx, task, func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
		return r.executeAt(ctx, f, task, interceptors, i+1)
	})
}
