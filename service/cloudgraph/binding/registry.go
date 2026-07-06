// SPDX-License-Identifier: MPL-2.0

// Package binding resolves provider_type strings to registered provider
// manifests (docs/design/cloud-graph.md §7, PoC subset).
package binding

import (
	"sync"

	cgapi "github.com/wippyai/runtime/api/service/cloudgraph"
)

// Registry is the only path from a provider_type to executable provider
// configuration. Manifests are immutable after registration; resolution
// fails hard on a miss (design I20).
type Registry struct {
	providers map[string]*cgapi.ProviderConfig
	mu        sync.RWMutex
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]*cgapi.ProviderConfig)}
}

// Register installs a validated manifest. A second manifest claiming an
// existing provider_type is a hard configuration error.
func (r *Registry) Register(cfg *cgapi.ProviderConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[cfg.ProviderType]; exists {
		return cgapi.NewProviderConflictError(cfg.ProviderType)
	}
	r.providers[cfg.ProviderType] = cfg
	return nil
}

// Provider resolves a provider_type to its manifest, failing hard on a miss.
func (r *Registry) Provider(providerType string) (*cgapi.ProviderConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cfg, ok := r.providers[providerType]
	if !ok {
		return nil, cgapi.NewProviderNotFoundError(providerType)
	}
	return cfg, nil
}
