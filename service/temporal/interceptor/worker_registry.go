package interceptor

import (
	"sync"

	"go.temporal.io/sdk/interceptor"
)

// WorkerRegistry manages worker interceptors for Temporal workers
type WorkerRegistry struct {
	interceptors []interceptor.WorkerInterceptor
	mu           sync.RWMutex
}

// NewWorkerRegistry creates a new worker interceptor registry
func NewWorkerRegistry() *WorkerRegistry {
	return &WorkerRegistry{
		interceptors: make([]interceptor.WorkerInterceptor, 0),
	}
}

// Register adds a worker interceptor to the registry
func (r *WorkerRegistry) Register(i interceptor.WorkerInterceptor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interceptors = append(r.interceptors, i)
}

// GetAll returns all registered worker interceptors
func (r *WorkerRegistry) GetAll() []interceptor.WorkerInterceptor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]interceptor.WorkerInterceptor, len(r.interceptors))
	copy(result, r.interceptors)
	return result
}
