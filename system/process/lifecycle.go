package process

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
)

// LifecycleRegistry manages multiple lifecycle handlers and calls them
// in registration order during process lifecycle events.
type LifecycleRegistry struct {
	mu       sync.RWMutex
	handlers []namedLifecycle
}

type namedLifecycle struct {
	name string
	lc   process.Lifecycle
}

// NewLifecycleRegistry creates a new lifecycle registry.
func NewLifecycleRegistry() *LifecycleRegistry {
	return &LifecycleRegistry{}
}

// Register adds a lifecycle handler with the given name.
// Handlers are called in registration order.
func (r *LifecycleRegistry) Register(name string, lc process.Lifecycle) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, h := range r.handlers {
		if h.name == name {
			r.handlers[i].lc = lc
			return
		}
	}
	r.handlers = append(r.handlers, namedLifecycle{name: name, lc: lc})
}

// Unregister removes a lifecycle handler by name.
func (r *LifecycleRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, h := range r.handlers {
		if h.name == name {
			r.handlers = append(r.handlers[:i], r.handlers[i+1:]...)
			return
		}
	}
}

// OnStart calls all registered lifecycle handlers' OnStart methods.
func (r *LifecycleRegistry) OnStart(ctx context.Context, pid pid.PID, proc process.Process) {
	r.mu.RLock()
	handlers := make([]process.Lifecycle, len(r.handlers))
	for i, h := range r.handlers {
		handlers[i] = h.lc
	}
	r.mu.RUnlock()

	for _, h := range handlers {
		h.OnStart(ctx, pid, proc)
	}
}

// OnComplete calls all registered lifecycle handlers' OnComplete methods.
func (r *LifecycleRegistry) OnComplete(ctx context.Context, pid pid.PID, result *runtime.Result) {
	r.mu.RLock()
	handlers := make([]process.Lifecycle, len(r.handlers))
	for i, h := range r.handlers {
		handlers[i] = h.lc
	}
	r.mu.RUnlock()

	for _, h := range handlers {
		h.OnComplete(ctx, pid, result)
	}
}

var _ process.LifecycleRegistry = (*LifecycleRegistry)(nil)
