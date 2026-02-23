// SPDX-License-Identifier: MPL-2.0

// Package interceptor provides registries for collecting and distributing
// Temporal SDK client and worker interceptors.
package interceptor

import (
	"sync"

	"go.temporal.io/sdk/interceptor"
)

// ClientRegistry collects client interceptors registered during boot and
// provides them as a snapshot to Temporal client construction.
type ClientRegistry struct {
	interceptors []interceptor.ClientInterceptor
	mu           sync.RWMutex
}

// NewClientRegistry creates a new client interceptor registry.
func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{
		interceptors: make([]interceptor.ClientInterceptor, 0),
	}
}

// Register appends a client interceptor to the registry.
func (r *ClientRegistry) Register(i interceptor.ClientInterceptor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interceptors = append(r.interceptors, i)
}

// GetAll returns a copy of all registered client interceptors.
func (r *ClientRegistry) GetAll() []interceptor.ClientInterceptor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]interceptor.ClientInterceptor, len(r.interceptors))
	copy(result, r.interceptors)
	return result
}

// WorkerRegistry collects worker interceptors registered during boot and
// provides them as a snapshot to Temporal worker construction.
type WorkerRegistry struct {
	interceptors []interceptor.WorkerInterceptor
	mu           sync.RWMutex
}

// NewWorkerRegistry creates a new worker interceptor registry.
func NewWorkerRegistry() *WorkerRegistry {
	return &WorkerRegistry{
		interceptors: make([]interceptor.WorkerInterceptor, 0),
	}
}

// Register appends a worker interceptor to the registry.
func (r *WorkerRegistry) Register(i interceptor.WorkerInterceptor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interceptors = append(r.interceptors, i)
}

// GetAll returns a copy of all registered worker interceptors.
func (r *WorkerRegistry) GetAll() []interceptor.WorkerInterceptor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]interceptor.WorkerInterceptor, len(r.interceptors))
	copy(result, r.interceptors)
	return result
}
