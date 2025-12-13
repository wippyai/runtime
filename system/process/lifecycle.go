package process

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
)

// LifecycleRegistry manages multiple lifecycle handlers and calls them
// in registration order during process lifecycle events.
// Uses copy-on-write for zero allocations on OnStart/OnComplete.
type LifecycleRegistry struct {
	mu       sync.Mutex
	named    []namedLifecycle                    // mutable list for registration
	handlers atomic.Pointer[[]process.Lifecycle] // immutable snapshot for iteration
}

type namedLifecycle struct {
	name string
	lc   process.Lifecycle
}

// NewLifecycleRegistry creates a new lifecycle registry.
func NewLifecycleRegistry() *LifecycleRegistry {
	r := &LifecycleRegistry{}
	empty := make([]process.Lifecycle, 0)
	r.handlers.Store(&empty)
	return r
}

// Register adds a lifecycle handler with the given name.
// Handlers are called in registration order.
func (r *LifecycleRegistry) Register(name string, lc process.Lifecycle) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, h := range r.named {
		if h.name == name {
			r.named[i].lc = lc
			r.rebuildSnapshot()
			return
		}
	}
	r.named = append(r.named, namedLifecycle{name: name, lc: lc})
	r.rebuildSnapshot()
}

// Unregister removes a lifecycle handler by name.
func (r *LifecycleRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, h := range r.named {
		if h.name == name {
			r.named = append(r.named[:i], r.named[i+1:]...)
			r.rebuildSnapshot()
			return
		}
	}
}

// rebuildSnapshot creates a new immutable slice from current named handlers.
// Must be called with mu held.
func (r *LifecycleRegistry) rebuildSnapshot() {
	snapshot := make([]process.Lifecycle, len(r.named))
	for i, h := range r.named {
		snapshot[i] = h.lc
	}
	r.handlers.Store(&snapshot)
}

// OnStart calls all registered lifecycle handlers' OnStart methods.
func (r *LifecycleRegistry) OnStart(ctx context.Context, pid pid.PID, proc process.Process) {
	handlers := *r.handlers.Load()
	for _, h := range handlers {
		h.OnStart(ctx, pid, proc)
	}
}

// OnComplete calls all registered lifecycle handlers' OnComplete methods.
func (r *LifecycleRegistry) OnComplete(ctx context.Context, pid pid.PID, result *runtime.Result) {
	handlers := *r.handlers.Load()
	for _, h := range handlers {
		h.OnComplete(ctx, pid, result)
	}
}

var _ process.LifecycleRegistry = (*LifecycleRegistry)(nil)
