// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"errors"

	envapi "github.com/wippyai/runtime/api/env"
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

// Manager routes overlay network registry entries to the Driver that handles
// their kind. Drivers are injected at construction time via WithDriver so the
// Manager stays decoupled from any particular network implementation.
type Manager struct {
	registry *netsystem.Registry
	drivers  map[registry.Kind]Driver
	log      *zap.Logger
	deps     Deps
}

// Option configures a Manager at construction time.
type Option func(*Manager)

// WithStateDir sets the base directory for driver-local state (tsnet node
// keys, I2P session files, etc.). Empty string leaves drivers with their
// upstream defaults.
func WithStateDir(dir string) Option {
	return func(m *Manager) { m.deps.StateDir = dir }
}

// WithDriver registers one or more drivers on the Manager. Later registrations
// for an already-registered kind replace the earlier one.
func WithDriver(drivers ...Driver) Option {
	return func(m *Manager) {
		for _, d := range drivers {
			if d == nil {
				continue
			}
			m.drivers[d.Kind()] = d
		}
	}
}

// NewManager creates a new network overlay manager. The env registry is used
// by drivers to resolve indirect credentials (e.g. Tailscale's AuthKeyEnv);
// pass nil when no driver in use requires it. Drivers must be registered via
// WithDriver — a Manager with no drivers rejects every entry with
// ErrUnsupportedKind.
func NewManager(
	reg *netsystem.Registry,
	dtt payload.Transcoder,
	env envapi.Registry,
	log *zap.Logger,
	opts ...Option,
) (*Manager, error) {
	if reg == nil {
		return nil, errors.New("network manager: registry required")
	}
	if dtt == nil {
		return nil, errors.New("network manager: transcoder required")
	}
	if log == nil {
		log = zap.NewNop()
	}
	m := &Manager{
		registry: reg,
		deps: Deps{
			Transcoder: dtt,
			Env:        env,
			Logger:     log,
		},
		drivers: map[registry.Kind]Driver{},
		log:     log,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m, nil
}

// --- registry.EntryListener ---

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	return m.createAndRegister(ctx, entry)
}

// Update creates the replacement service first and only swaps it into the
// registry if creation succeeds. On failure the previous service is left
// running — hot-reload never tears the old one down just because the new
// config was bad.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	driver, ok := m.drivers[entry.Kind]
	if !ok {
		return NewUnsupportedKindError(entry.Kind)
	}

	svc, err := driver.Create(ctx, entry, m.deps)
	if err != nil {
		m.log.Error("failed to create replacement network service",
			zap.String("id", entry.ID.String()),
			zap.String("kind", entry.Kind),
			zap.Error(err),
		)
		return err
	}

	m.registry.Replace(entry.ID, svc, entry.Kind)
	return nil
}

func (m *Manager) Delete(_ context.Context, entry registry.Entry) error {
	m.registry.Unregister(entry.ID)
	return nil
}

// --- registry.TransactionListener ---

func (m *Manager) Begin(_ context.Context) error   { return nil }
func (m *Manager) Commit(_ context.Context) error  { return nil }
func (m *Manager) Discard(_ context.Context) error { return nil }

// --- internal ---

func (m *Manager) createAndRegister(ctx context.Context, entry registry.Entry) error {
	driver, ok := m.drivers[entry.Kind]
	if !ok {
		return NewUnsupportedKindError(entry.Kind)
	}

	svc, err := driver.Create(ctx, entry, m.deps)
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
