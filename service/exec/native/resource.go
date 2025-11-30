package native

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/service/exec"
)

// executorProvider implements resource.Provider for ProcessExecutor
type executorProvider struct {
	executor exec.ProcessExecutor
	mu       sync.RWMutex
	closed   bool
}

// Acquire implements resource.Provider.Acquire
func (p *executorProvider) Acquire(_ context.Context, _ registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, resource.ErrResourceClosed
	}

	// Currently we don't implement locking or exclusive mode
	// Future implementations could support exclusive access
	if mode == resource.ModeExclusive {
		return nil, resource.ErrResourceLocked
	}

	// Return a resource wrapper for the executor
	return &executorResource{
		executor: p.executor,
	}, nil
}

// Close closes the provider and releases resources
func (p *executorProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	p.closed = true
	return nil
}

// executorResource implements resource.Resource for ProcessExecutor
type executorResource struct {
	executor exec.ProcessExecutor
	mu       sync.Mutex
	released bool
}

// Get implements resource.Resource.Get
func (r *executorResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.released {
		return nil, resource.ErrResourceReleased
	}

	return r.executor, nil
}

// Release implements resource.Resource.Release
func (r *executorResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.released {
		return
	}

	r.released = true
}

// newExecutorProvider creates a new provider for process executors
func newExecutorProvider(executor exec.ProcessExecutor) *executorProvider {
	return &executorProvider{
		executor: executor,
	}
}
