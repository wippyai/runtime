package boot

import (
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/boot"
)

var (
	globalRegistry = &registry{
		plugins: make(map[string]boot.Plugin),
	}
)

type registry struct {
	mu      sync.RWMutex
	plugins map[string]boot.Plugin
}

// Register adds a plugin to the global registry.
func Register(p boot.Plugin) error {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	name := p.Name()
	if _, exists := globalRegistry.plugins[name]; exists {
		return fmt.Errorf("plugin %q already registered", name)
	}

	globalRegistry.plugins[name] = p
	return nil
}

// MustRegister registers a plugin or panics.
func MustRegister(p boot.Plugin) {
	if err := Register(p); err != nil {
		panic(err)
	}
}

// New creates a loader with all registered plugins.
func New() *Loader {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	loader := NewLoader()

	for _, p := range globalRegistry.plugins {
		if err := loader.Register(p); err != nil {
			panic(fmt.Sprintf("failed to register plugin: %v", err))
		}
	}

	return loader
}
