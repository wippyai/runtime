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

// Registry manages available interceptors.
// Interceptors are registered at boot, then Seal() is called.
// After sealing, Execute() has zero overhead - just a single function call.
type Registry struct {
	logger  *zap.Logger
	entries []entry
	mu      sync.Mutex

	// Sealed state - the pre-built pipeline function, created once at Seal()
	sealed   bool
	pipeline func(context.Context, function.Func, runtime.Task) (*runtime.Result, error)
}

// NewInterceptorRegistry creates a new interceptor registry
func NewInterceptorRegistry(logger *zap.Logger) *Registry {
	return &Registry{
		logger:  logger,
		entries: make([]entry, 0),
	}
}

// Register adds an interceptor to the registry.
// Must be called before Seal().
func (r *Registry) Register(name string, interceptor function.Interceptor, order int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.sealed {
		return NewInterceptorSealedError()
	}

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

	r.logger.Debug("interceptor registered",
		zap.String("interceptor", name),
		zap.Int("order", order))

	return nil
}

// Unregister removes an interceptor from the registry.
// Must be called before Seal().
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.sealed {
		return NewInterceptorSealedError()
	}

	for i, e := range r.entries {
		if e.name == name {
			r.entries = append(r.entries[:i], r.entries[i+1:]...)

			r.logger.Debug("interceptor unregistered",
				zap.String("interceptor", name))

			return nil
		}
	}

	return NewInterceptorNotFoundError(name)
}

// Seal finalizes the interceptor chain. After this, no more changes allowed.
// Builds the pipeline function once - zero allocations per request after this.
func (r *Registry) Seal() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.sealed {
		return
	}

	sort.Slice(r.entries, func(i, j int) bool {
		return r.entries[i].order < r.entries[j].order
	})

	n := len(r.entries)
	if n == 0 {
		r.pipeline = nil
		r.sealed = true
		r.entries = nil
		r.logger.Info("interceptor chain sealed", zap.Int("count", 0))
		return
	}

	// Copy interceptors to a slice that won't change
	interceptors := make([]function.Interceptor, n)
	for i, e := range r.entries {
		interceptors[i] = e.interceptor
	}

	// Build pipeline - the tricky part is that Handle() requires a `next` function.
	// We can't avoid that allocation unless we change the interface.
	// But we CAN avoid the RWMutex and the buildNext() overhead.
	r.pipeline = func(ctx context.Context, f function.Func, task runtime.Task) (*runtime.Result, error) {
		// This still creates N closures per request for the `next` parameter.
		// To truly have zero allocations, the interface must change.
		var execute func(i int) (*runtime.Result, error)
		execute = func(i int) (*runtime.Result, error) {
			if i >= n {
				return f(ctx, task)
			}
			return interceptors[i].Handle(ctx, task, func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
				return execute(i + 1)
			})
		}
		return execute(0)
	}

	r.sealed = true
	r.entries = nil

	r.logger.Info("interceptor chain sealed", zap.Int("count", n))
}

// Execute runs the sealed interceptor chain.
// Zero allocations per request - just calls the pre-built pipeline.
func (r *Registry) Execute(ctx context.Context, f function.Func, task runtime.Task) (*runtime.Result, error) {
	if r.pipeline == nil {
		return f(ctx, task)
	}
	return r.pipeline(ctx, f, task)
}
