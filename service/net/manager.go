// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// Compile-time interface checks.
var (
	_ registry.EntryListener       = (*Manager)(nil)
	_ registry.TransactionListener = (*Manager)(nil)
	_ netapi.NetworkRegistry       = (*Manager)(nil)
)

// networkEntry holds a running network service and its metadata.
type networkEntry struct {
	service netapi.Service
	kind    registry.Kind
}

// Manager manages network overlay service lifecycles.
// It implements registry.EntryListener to react to network entry changes
// and netapi.NetworkRegistry to provide service lookup for consumers.
type Manager struct {
	dtt      payload.Transcoder
	log      *zap.Logger
	services map[registry.ID]*networkEntry
	mu       sync.RWMutex
}

// NewManager creates a new network overlay manager.
func NewManager(dtt payload.Transcoder, log *zap.Logger) (*Manager, error) {
	if dtt == nil {
		return nil, fmt.Errorf("network manager: transcoder required")
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &Manager{
		dtt:      dtt,
		log:      log,
		services: make(map[registry.ID]*networkEntry),
	}, nil
}

// --- registry.EntryListener ---

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createService(ctx, entry)
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopService(entry.ID)
	return m.createService(ctx, entry)
}

func (m *Manager) Delete(_ context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopService(entry.ID)
	return nil
}

// --- registry.TransactionListener ---

func (m *Manager) Begin(_ context.Context)   {}
func (m *Manager) Commit(_ context.Context)  {}
func (m *Manager) Discard(_ context.Context) {}

// --- netapi.NetworkRegistry ---

func (m *Manager) GetNetwork(id registry.ID) (netapi.Service, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.services[id]
	if !ok {
		return nil, netapi.ErrNetworkNotFound
	}
	return entry.service, nil
}

func (m *Manager) HasNetwork(id registry.ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.services[id]
	return ok
}

func (m *Manager) NetworkKind(id registry.ID) registry.Kind {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.services[id]
	if !ok {
		return ""
	}
	return entry.kind
}

// --- internal ---

func (m *Manager) createService(_ context.Context, entry registry.Entry) error {
	var svc netapi.Service
	var err error

	switch entry.Kind {
	case netapi.KindTor:
		svc, err = m.createTor(entry)
	case netapi.KindI2P:
		svc, err = m.createI2P(entry)
	case netapi.KindTailscale:
		svc, err = m.createTailscale(entry)
	default:
		return fmt.Errorf("network manager: unsupported kind %q", entry.Kind)
	}

	if err != nil {
		m.log.Error("failed to create network service",
			zap.String("id", entry.ID.String()),
			zap.String("kind", entry.Kind),
			zap.Error(err),
		)
		return err
	}

	m.services[entry.ID] = &networkEntry{
		service: svc,
		kind:    entry.Kind,
	}

	m.log.Info("network service created",
		zap.String("id", entry.ID.String()),
		zap.String("kind", entry.Kind),
	)
	return nil
}

func (m *Manager) stopService(id registry.ID) {
	entry, ok := m.services[id]
	if !ok {
		return
	}
	if closer, ok := entry.service.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			m.log.Warn("error closing network service",
				zap.String("id", id.String()),
				zap.Error(err),
			)
		}
	}
	delete(m.services, id)
	m.log.Debug("network service stopped", zap.String("id", id.String()))
}

func (m *Manager) decodeConfig(entry registry.Entry, target any) error {
	if entry.Data == nil {
		return fmt.Errorf("network manager: entry %s has no data", entry.ID.String())
	}
	if err := m.dtt.Unmarshal(entry.Data, target); err != nil {
		return err
	}
	if metaHolder, ok := target.(interface{ SetMeta(attrs.Bag) }); ok {
		metaHolder.SetMeta(entry.Meta)
	}
	return nil
}

func (m *Manager) createTor(entry registry.Entry) (netapi.Service, error) {
	var cfg netapi.TorConfig
	if err := m.decodeConfig(entry, &cfg); err != nil {
		return nil, fmt.Errorf("tor config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("tor config: %w", err)
	}
	return NewTorService(&cfg)
}

func (m *Manager) createI2P(entry registry.Entry) (netapi.Service, error) {
	var cfg netapi.I2PConfig
	if err := m.decodeConfig(entry, &cfg); err != nil {
		return nil, fmt.Errorf("i2p config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("i2p config: %w", err)
	}
	return NewI2PService(&cfg)
}

func (m *Manager) createTailscale(entry registry.Entry) (netapi.Service, error) {
	var cfg netapi.TailscaleConfig
	if err := m.decodeConfig(entry, &cfg); err != nil {
		return nil, fmt.Errorf("tailscale config: %w", err)
	}
	return NewTailscaleService(&cfg)
}
