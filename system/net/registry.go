// SPDX-License-Identifier: MPL-2.0

package net

import (
	"io"
	"sync"

	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// Compile-time interface check.
var _ netapi.NetworkRegistry = (*Registry)(nil)

// entry holds a running network service and its metadata.
type entry struct {
	service netapi.Service
	kind    registry.Kind
}

// Registry stores and retrieves overlay network services by registry ID.
// It knows nothing about how services are created — that is the
// responsibility of service-level drivers.
type Registry struct {
	log      *zap.Logger
	services map[registry.ID]*entry
	mu       sync.RWMutex
}

// NewRegistry creates a new network overlay registry.
func NewRegistry(log *zap.Logger) *Registry {
	if log == nil {
		log = zap.NewNop()
	}
	return &Registry{
		log:      log,
		services: make(map[registry.ID]*entry),
	}
}

// Register adds a network service to the registry.
func (r *Registry) Register(id registry.ID, svc netapi.Service, kind registry.Kind) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[id] = &entry{service: svc, kind: kind}
	r.log.Info("network service registered",
		zap.String("id", id.String()),
		zap.String("kind", kind),
	)
}

// Replace swaps the service registered under id with svc in a single
// critical section, closing the previous service (if any). Concurrent
// readers see either the old or the new service — never a missing one.
// Used by the Manager on entry Update so hot-reload doesn't create a
// visibility gap for in-flight overlay lookups.
func (r *Registry) Replace(id registry.ID, svc netapi.Service, kind registry.Kind) {
	r.mu.Lock()
	old, had := r.services[id]
	r.services[id] = &entry{service: svc, kind: kind}
	r.mu.Unlock()

	if had {
		if closer, ok := old.service.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				r.log.Warn("error closing previous network service on replace",
					zap.String("id", id.String()),
					zap.Error(err),
				)
			}
		}
		r.log.Info("network service replaced",
			zap.String("id", id.String()),
			zap.String("kind", kind),
		)
		return
	}
	r.log.Info("network service registered",
		zap.String("id", id.String()),
		zap.String("kind", kind),
	)
}

// Unregister removes a network service from the registry, closing it if possible.
func (r *Registry) Unregister(id registry.ID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.services[id]
	if !ok {
		return
	}
	if closer, ok := e.service.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			r.log.Warn("error closing network service",
				zap.String("id", id.String()),
				zap.Error(err),
			)
		}
	}
	delete(r.services, id)
	r.log.Debug("network service unregistered", zap.String("id", id.String()))
}

// --- netapi.NetworkRegistry (read-only interface) ---

// GetNetwork returns the Service for the given network registry ID.
func (r *Registry) GetNetwork(id registry.ID) (netapi.Service, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.services[id]
	if !ok {
		return nil, netapi.ErrNetworkNotFound
	}
	return e.service, nil
}

// HasNetwork returns true if a network with the given ID is registered.
func (r *Registry) HasNetwork(id registry.ID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.services[id]
	return ok
}

// NetworkKind returns the registry kind of the network with the given ID.
func (r *Registry) NetworkKind(id registry.ID) registry.Kind {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.services[id]
	if !ok {
		return ""
	}
	return e.kind
}
