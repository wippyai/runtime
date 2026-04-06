// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	netsystem "github.com/wippyai/runtime/system/net"
	"go.uber.org/zap"
)

// Compile-time interface checks.
var (
	_ registry.EntryListener       = (*Manager)(nil)
	_ registry.TransactionListener = (*Manager)(nil)
)

// Manager creates overlay network driver instances from registry entries
// and registers them with the system-level network Registry.
type Manager struct {
	registry *netsystem.Registry
	dtt      payload.Transcoder
	log      *zap.Logger
}

// NewManager creates a new network overlay manager.
func NewManager(reg *netsystem.Registry, dtt payload.Transcoder, log *zap.Logger) (*Manager, error) {
	if reg == nil {
		return nil, fmt.Errorf("network manager: registry required")
	}
	if dtt == nil {
		return nil, fmt.Errorf("network manager: transcoder required")
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &Manager{
		registry: reg,
		dtt:      dtt,
		log:      log,
	}, nil
}

// --- registry.EntryListener ---

func (m *Manager) Add(_ context.Context, entry registry.Entry) error {
	return m.createAndRegister(entry)
}

func (m *Manager) Update(_ context.Context, entry registry.Entry) error {
	m.registry.Unregister(entry.ID)
	return m.createAndRegister(entry)
}

func (m *Manager) Delete(_ context.Context, entry registry.Entry) error {
	m.registry.Unregister(entry.ID)
	return nil
}

// --- registry.TransactionListener ---

func (m *Manager) Begin(_ context.Context)   {}
func (m *Manager) Commit(_ context.Context)  {}
func (m *Manager) Discard(_ context.Context) {}

// --- internal ---

func (m *Manager) createAndRegister(entry registry.Entry) error {
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

	m.registry.Register(entry.ID, svc, entry.Kind)
	return nil
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
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("tailscale config: %w", err)
	}
	return NewTailscaleService(&cfg)
}
