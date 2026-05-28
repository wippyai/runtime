// SPDX-License-Identifier: MPL-2.0

// Package pg implements distributed named process groups as a service.
//
// It contains both the scope Manager (a registry.EntryListener that creates
// one independent process-group instance per pg.Scope entry) and the Service
// engine each instance runs: a single-writer event loop, eventually-consistent
// cross-node membership, broadcast, and monitor/events. Semantics follow
// Erlang/OTP's pg module.
package pg

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/topology"
	entryutil "github.com/wippyai/runtime/internal/entry"

	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

// Manager manages PG scope instances. It implements registry.EntryListener
// to react to registry entries of kind pg.Scope. Each entry creates an
// independent PG service instance with its own state, event loop, and
// cluster mesh — matching Erlang/OTP's pg:start_link(ScopeName) semantics.
type Manager struct {
	bus        event.Bus
	dtt        payload.Transcoder
	topo       topology.Topology
	membership cluster.Membership
	log        *zap.Logger
	scopes     map[registry.ID]*Service
	localNode  pid.NodeID
	mu         sync.RWMutex
}

// NewManager creates a new PG scope manager.
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	topo topology.Topology,
	membership cluster.Membership,
	localNode pid.NodeID,
	log *zap.Logger,
) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
	return &Manager{
		bus:        bus,
		dtt:        dtt,
		topo:       topo,
		membership: membership,
		localNode:  localNode,
		log:        log,
		scopes:     make(map[registry.ID]*Service),
	}
}

// Add implements registry.EntryListener. It creates a new PG scope from
// a registry entry, registers it as a relay host and supervised service,
// and makes it available as a resource provider.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != pgapi.Scope {
		return NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.scopes[entry.ID]; exists {
		return NewScopeAlreadyExistsError(entry.ID.String())
	}

	// Decode and validate configuration
	cfg, err := entryutil.DecodeEntryConfig[pgapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return NewDecodeConfigError(err)
	}

	// The registry entry ID becomes the relay host ID.
	// E.g. registry ID "pg" → HostID "pg", "pg.chat" → HostID "pg.chat".
	hostID := entry.ID.String()

	// Resolve the relay router from the node.
	// The manager receives a relay.Receiver (the router) indirectly
	// via the node, but we need a relay.Receiver for the service.
	// We'll use the node's relay router which is obtained from context
	// at boot time and passed into the service.
	router := relay.GetRouter(ctx)

	// Create the PG service instance, wired to the metrics collector and
	// global OTel providers so pg_* series flow when ops happen.
	svc := NewService(
		m.log, hostID, cfg, router, m.topo, m.membership, m.bus, m.localNode,
		metricsapi.GetCollector(ctx),
		otel.GetMeterProvider(),
		otel.GetTracerProvider(),
	)
	m.scopes[entry.ID] = svc

	// Register as relay host so inter-node messages route to this scope
	m.bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostRegister,
		Path:   entry.ID.String(),
		Data:   relay.Receiver(svc),
	})

	// Register with supervisor for lifecycle management
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: svc,
			Config:  cfg.Lifecycle,
		},
	})

	// Register as resource provider so Lua can acquire PG instances
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Provider: svc,
			Meta:     entry.Meta,
		},
	})

	m.log.Info("pg scope added",
		zap.String("id", entry.ID.String()),
		zap.String("host_id", hostID),
	)

	return nil
}

// Update implements registry.EntryListener. It replaces an existing PG scope
// by deleting and re-adding it.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != pgapi.Scope {
		return NewUnsupportedKindError(entry.Kind)
	}
	if err := m.Delete(ctx, entry); err != nil {
		return err
	}
	return m.Add(ctx, entry)
}

// Delete implements registry.EntryListener. It stops and removes a PG scope,
// unregistering it from the relay, supervisor, and resource systems.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != pgapi.Scope {
		return NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	svc, exists := m.scopes[entry.ID]
	if !exists {
		m.mu.Unlock()
		return NewScopeNotFoundError(entry.ID.String())
	}
	delete(m.scopes, entry.ID)
	m.mu.Unlock()

	// Unregister from supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   entry.ID.String(),
	})

	// Unregister relay host
	m.bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostDelete,
		Path:   entry.ID.String(),
	})

	// Unregister resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   entry.ID.String(),
		Data:   entry.ID,
	})

	// Stop the service
	if err := svc.Stop(ctx); err != nil {
		m.log.Warn("failed to stop pg scope cleanly",
			zap.String("id", entry.ID.String()),
			zap.Error(err),
		)
	}

	m.log.Info("pg scope deleted",
		zap.String("id", entry.ID.String()),
	)

	return nil
}

// GetScope returns a PG service by registry ID.
func (m *Manager) GetScope(id registry.ID) (*Service, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	svc, ok := m.scopes[id]
	return svc, ok
}
