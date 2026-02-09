package transport

import (
	"fmt"
	"strings"
	"sync"
)

// Registry stores named WASM transports for runtime lookup.
type Registry struct {
	transports map[string]any
	mu         sync.RWMutex
}

// NewRegistry creates an empty transport registry.
func NewRegistry() *Registry {
	return &Registry{
		transports: make(map[string]any),
	}
}

// Register adds a transport under a normalized name.
func (r *Registry) Register(name string, t any) error {
	key := normalizeName(name)
	if key == "" {
		return fmt.Errorf("transport name cannot be empty")
	}
	if t == nil {
		return fmt.Errorf("transport %q cannot be nil", key)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.transports[key]; exists {
		return fmt.Errorf("transport already registered: %s", key)
	}
	r.transports[key] = t
	return nil
}

// Get returns transport by normalized name.
func (r *Registry) Get(name string) (any, bool) {
	key := normalizeName(name)
	r.mu.RLock()
	t, ok := r.transports[key]
	r.mu.RUnlock()
	return t, ok
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
