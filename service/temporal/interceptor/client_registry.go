package interceptor

import (
	"sync"

	"go.temporal.io/sdk/interceptor"
)

// ClientRegistry manages client interceptors for Temporal clients
type ClientRegistry struct {
	mu           sync.RWMutex
	interceptors []interceptor.ClientInterceptor
}

// NewClientRegistry creates a new client interceptor registry
func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{
		interceptors: make([]interceptor.ClientInterceptor, 0),
	}
}

// Register adds a client interceptor to the registry
func (r *ClientRegistry) Register(i interceptor.ClientInterceptor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interceptors = append(r.interceptors, i)
}

// GetAll returns all registered client interceptors
func (r *ClientRegistry) GetAll() []interceptor.ClientInterceptor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]interceptor.ClientInterceptor, len(r.interceptors))
	copy(result, r.interceptors)
	return result
}
