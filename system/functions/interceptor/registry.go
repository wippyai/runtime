package interceptor

import (
	"fmt"
	"sync"
)

// Registry manages available interceptors
type Registry struct {
	interceptors sync.Map // map[string]Interceptor
}

// NewRegistry creates a new interceptor registry
func NewRegistry() *Registry {
	return &Registry{}
}

// Register registers an interceptor with the given name
func (r *Registry) Register(name string, interceptor Interceptor) error {
	if _, loaded := r.interceptors.LoadOrStore(name, interceptor); loaded {
		return fmt.Errorf("interceptor %s already registered", name)
	}
	return nil
}

// Get returns an interceptor by name
func (r *Registry) Get(name string) (Interceptor, error) {
	if value, ok := r.interceptors.Load(name); ok {
		if interceptor, ok := value.(Interceptor); ok {
			return interceptor, nil
		}
		return nil, fmt.Errorf("invalid interceptor type for %s", name)
	}
	return nil, fmt.Errorf("interceptor %s not found", name)
}

// List returns all registered interceptor names
func (r *Registry) List() []string {
	var names []string
	r.interceptors.Range(func(key, _ interface{}) bool {
		if name, ok := key.(string); ok {
			names = append(names, name)
		}
		return true
	})
	return names
}

// Unregister removes an interceptor by name
func (r *Registry) Unregister(name string) error {
	if _, loaded := r.interceptors.LoadAndDelete(name); !loaded {
		return fmt.Errorf("interceptor %s not found", name)
	}
	return nil
}
