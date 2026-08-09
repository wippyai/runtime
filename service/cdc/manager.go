// SPDX-License-Identifier: MPL-2.0

// Package cdc contains the driver router and lifecycle owner for CDC sources.
// Database-specific packages implement Driver; this package never imports a
// concrete database implementation.
package cdc

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/cdc"
	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
)

var (
	ErrRegistryRequired   = errors.New("cdc manager: registry is required")
	ErrEventBusRequired   = errors.New("cdc manager: event bus is required")
	ErrDriverRequired     = errors.New("cdc manager: driver is required")
	ErrUnsupportedKind    = errors.New("cdc manager: unsupported source kind")
	ErrSourceExists       = errors.New("cdc manager: source already exists")
	ErrSourceNotFound     = errors.New("cdc manager: source not found")
	ErrSourceKindChange   = errors.New("cdc manager: source kind cannot change")
	ErrSourceKindMismatch = errors.New("cdc manager: source kind does not match entry")
	ErrExclusiveOwned     = errors.New("cdc manager: exclusive resource is already owned")
)

// Dependencies are the shared collaborators available to concrete drivers.
// The manager owns lifecycle event emission; drivers only construct sources.
type Dependencies struct {
	Transcoder payload.Transcoder
	Resources  resource.Registry
	Logger     *zap.Logger
}

// ManagedSource is the internal source returned by a driver. Source
// construction and lifecycle remain separate from the system registry, while
// one value is used by both the public source API and supervisor.
type ManagedSource interface {
	api.Source
	supervisor.Service
}

// Registry is the mutable capability the manager needs from the system CDC
// registry. Keeping the concrete system implementation behind this interface
// lets boot and tests inject the canonical registry without coupling the
// service package to a particular registry implementation.
type Registry interface {
	api.Registry
	Register(registry.ID, api.Source, registry.Kind) error
	Unregister(registry.ID) (api.Source, bool)
}

// Disposable is an optional destructive-delete hook. It is invoked only for
// manager.Delete, while the source remains as a non-subscribable tombstone;
// registry removal commits only after disposal succeeds. Ordinary
// Stop/replacement never calls it, which lets drivers retain durable resources
// such as PostgreSQL replication slots across restart and update while still
// cleaning them up on dynamic uninstall.
type Disposable interface {
	Dispose(context.Context) error
}

// ExclusiveResource identifies a resource that cannot be held by two source
// generations at once (for example, a persistent PostgreSQL replication slot).
// Sources with different keys can be started before the registry swap. Sources
// with the same non-empty key use the slot's stop-start-restart handoff.
type ExclusiveResource interface {
	ExclusiveResourceKey() string
}

// Driver constructs a source for one registry kind. Drivers are injected when
// the manager is built; package initialization must not mutate global routing.
type Driver interface {
	Kind() registry.Kind
	Create(context.Context, registry.Entry, Dependencies) (ManagedSource, error)
}

// Option configures a Manager.
type Option func(*Manager)

// WithDriver injects one or more concrete source drivers. A later driver for
// the same kind intentionally replaces an earlier test or extension driver.
func WithDriver(drivers ...Driver) Option {
	return func(m *Manager) {
		for _, driver := range drivers {
			if driver == nil {
				continue
			}
			m.drivers[driver.Kind()] = driver
		}
	}
}

// Manager routes registry entries to injected drivers and owns their
// supervisor/system-registry lifecycle.
type Manager struct {
	registry Registry
	bus      event.Bus
	deps     Dependencies
	log      *zap.Logger
	drivers  map[registry.Kind]Driver
	leases   map[string]resourceLease
	leaseSeq uint64
	mu       sync.Mutex
}

type resourceLease struct {
	id    registry.ID
	token uint64
}

func NewManager(
	reg Registry,
	dtt payload.Transcoder,
	bus event.Bus,
	resources resource.Registry,
	log *zap.Logger,
	opts ...Option,
) (*Manager, error) {
	if reg == nil {
		return nil, ErrRegistryRequired
	}
	if bus == nil {
		return nil, ErrEventBusRequired
	}
	if log == nil {
		log = zap.NewNop()
	}
	m := &Manager{
		registry: reg,
		bus:      bus,
		deps: Dependencies{
			Transcoder: dtt,
			Resources:  resources,
			Logger:     log,
		},
		log:     log,
		drivers: make(map[registry.Kind]Driver),
		leases:  make(map[string]resourceLease),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	return m, nil
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	driver, ok := m.drivers[entry.Kind]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnsupportedKind, entry.Kind)
	}
	id := canonicalID(entry.ID)
	if _, exists := m.registry.Get(id); exists {
		return fmt.Errorf("%w: %s", ErrSourceExists, id.String())
	}

	source, err := driver.Create(ctx, entry, m.deps)
	if err != nil {
		return err
	}
	if isNilSource(source) {
		return ErrDriverRequired
	}
	key := exclusiveResourceKey(source)
	leaseToken, err := m.reserveLeaseLocked(key, id)
	if err != nil {
		_ = stopSource(ctx, source)
		return err
	}
	slot := newSourceSlot(id, entry.Kind, source, m.log.With(zap.String("id", id.String())))
	slot.setRetiredCleanupHook(func(retiredKey string, retiredToken uint64) {
		go m.releaseLease(id, retiredKey, retiredToken)
	})
	if err := m.registry.Register(id, slot, entry.Kind); err != nil {
		m.releaseLeaseLocked(id, key, leaseToken)
		_ = source.Stop(ctx)
		return err
	}
	m.registerSupervisor(ctx, id, slot)
	m.log.Info("added cdc source", zap.String("id", id.String()), zap.String("kind", entry.Kind))
	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := canonicalID(entry.ID)
	existing, exists := m.registry.Get(id)
	if exists {
		existingKind, knownKind := sourceKind(existing)
		if !knownKind {
			return errors.New("cdc manager: registered source has no kind")
		}
		if existingKind != entry.Kind {
			return fmt.Errorf("%w: %s -> %s", ErrSourceKindChange, existingKind, entry.Kind)
		}
	}
	driver, ok := m.drivers[entry.Kind]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnsupportedKind, entry.Kind)
	}
	if !exists {
		return fmt.Errorf("%w: %s", ErrSourceNotFound, id.String())
	}
	// Build the replacement before changing visibility. A malformed entry or
	// failed dependency acquisition leaves the old source untouched.
	replacement, err := driver.Create(ctx, entry, m.deps)
	if err != nil {
		return err
	}
	if isNilSource(replacement) {
		return ErrDriverRequired
	}
	managedSlot, ok := existing.(*sourceSlot)
	if !ok {
		_ = replacement.Stop(ctx)
		return errors.New("cdc manager: source is not managed by a stable slot")
	}
	oldKey := exclusiveResourceKey(managedSlot.currentSource())
	newKey := exclusiveResourceKey(replacement)
	oldToken := m.leaseTokenLocked(oldKey, id)
	reservedNew := oldKey != newKey
	newToken := oldToken
	if reservedNew {
		if managedSlot.hasRetiredKey(newKey) {
			_ = stopSource(ctx, replacement)
			return ErrSourceBusy
		}
		newToken, err = m.reserveLeaseLocked(newKey, id)
		if err != nil {
			_ = stopSource(ctx, replacement)
			return err
		}
	}
	oldLifecycle := normalizeLifecycleConfig(managedSlot.LifecycleConfig())
	if replaceErr := managedSlot.Replace(ctx, replacement, oldToken); replaceErr != nil {
		// A failed candidate start leaves the old generation current and the
		// speculative lease can be released. A retired-resource cleanup error
		// leaves the candidate current but faulted; retain its lease until it is
		// healthy or deleted.
		committed := managedSlot.currentSource() == replacement
		if reservedNew && !committed {
			m.releaseLeaseLocked(id, newKey, newToken)
		}
		if committed {
			m.reconfigureSupervisorIfChanged(ctx, id, managedSlot, oldLifecycle)
		}
		return replaceErr
	}
	if oldKey != newKey && !managedSlot.hasRetiredKey(oldKey) {
		m.releaseLeaseLocked(id, oldKey, oldToken)
	}
	m.reconfigureSupervisorIfChanged(ctx, id, managedSlot, oldLifecycle)
	m.log.Info("updated cdc source", zap.String("id", id.String()), zap.String("kind", entry.Kind))
	return nil
}

func (m *Manager) reconfigureSupervisorIfChanged(ctx context.Context, id registry.ID, source *sourceSlot, old supervisor.LifecycleConfig) {
	newLifecycle := normalizeLifecycleConfig(source.LifecycleConfig())
	if reflect.DeepEqual(old, newLifecycle) {
		return
	}
	// ServiceUpdate is emitted by the supervisor for status changes and is
	// not a reconfiguration primitive. Re-register the same stable slot in
	// order, so its controller rebuilds security/dependency/autostart state
	// without exposing a second source identity.
	m.unregisterSupervisor(ctx, id)
	m.registerSupervisorWithConfig(ctx, id, source, newLifecycle)
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := canonicalID(entry.ID)
	source, ok := m.registry.Get(id)
	if !ok {
		return fmt.Errorf("%w: %s", ErrSourceNotFound, id.String())
	}
	if entry.Kind != "" {
		kind, known := sourceKind(source)
		if !known || kind != entry.Kind {
			return fmt.Errorf("%w: %s", ErrSourceKindMismatch, entry.Kind)
		}
	}
	var err error
	if disposable, ok := source.(Disposable); ok {
		err = disposable.Dispose(ctx)
	} else {
		err = stopSource(ctx, source)
	}
	if err != nil {
		m.log.Warn("cdc source failed to stop during delete",
			zap.String("id", id.String()), zap.Error(err))
		return err
	}
	if _, ok := m.registry.Unregister(id); !ok {
		return fmt.Errorf("%w: %s", ErrSourceNotFound, id.String())
	}
	if slot, ok := source.(*sourceSlot); ok {
		for _, key := range slot.resourceKeys() {
			m.releaseLeaseLocked(id, key, m.leaseTokenLocked(key, id))
		}
	}
	m.unregisterSupervisor(ctx, id)
	m.log.Info("removed cdc source", zap.String("id", id.String()))
	return nil
}

func (m *Manager) List() []api.SourceInfo {
	return m.registry.List()
}

func (m *Manager) Get(id registry.ID) (api.Source, bool) {
	return m.registry.Get(id)
}

func (m *Manager) registerSupervisor(ctx context.Context, id registry.ID, source ManagedSource) {
	cfg := supervisor.LifecycleConfig{}
	if configured, ok := source.(interface {
		LifecycleConfig() supervisor.LifecycleConfig
	}); ok {
		cfg = configured.LifecycleConfig()
	}
	m.registerSupervisorWithConfig(ctx, id, source, normalizeLifecycleConfig(cfg))
}

func (m *Manager) registerSupervisorWithConfig(ctx context.Context, id registry.ID, source ManagedSource, cfg supervisor.LifecycleConfig) {
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   id.String(),
		Data: &supervisor.Entry{
			Service: source,
			Config:  cfg,
		},
	})
}

func (m *Manager) unregisterSupervisor(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   id.String(),
	})
}

func (m *Manager) reserveLeaseLocked(key string, id registry.ID) (uint64, error) {
	if key == "" {
		return 0, nil
	}
	if owner, ok := m.leases[key]; ok {
		if owner.id != id {
			return 0, fmt.Errorf("%w: %s (owner %s)", ErrExclusiveOwned, key, owner.id)
		}
		return owner.token, nil
	}
	m.leaseSeq++
	owner := resourceLease{id: id, token: m.leaseSeq}
	m.leases[key] = owner
	return owner.token, nil
}

func (m *Manager) releaseLease(id registry.ID, key string, token uint64) {
	m.mu.Lock()
	m.releaseLeaseLocked(id, key, token)
	m.mu.Unlock()
}

func (m *Manager) releaseLeaseLocked(id registry.ID, key string, token uint64) {
	if key == "" {
		return
	}
	if owner, ok := m.leases[key]; ok && owner.id == id && owner.token == token {
		delete(m.leases, key)
	}
}

func (m *Manager) leaseTokenLocked(key string, id registry.ID) uint64 {
	if key == "" {
		return 0
	}
	if owner, ok := m.leases[key]; ok && owner.id == id {
		return owner.token
	}
	return 0
}

func stopSource(ctx context.Context, source api.Source) error {
	if isNilSource(source) {
		return nil
	}
	if managed, ok := source.(supervisor.Service); ok {
		return managed.Stop(ctx)
	}
	return nil
}

func isNilSource(source api.Source) bool {
	if source == nil {
		return true
	}
	v := reflect.ValueOf(source)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func canonicalID(id registry.ID) registry.ID {
	return registry.ParseID(id.String())
}

func sourceKind(source api.Source) (registry.Kind, bool) {
	if isNilSource(source) {
		return "", false
	}
	if slot, ok := source.(*sourceSlot); ok {
		return slot.kind, slot.kind != ""
	}
	info := source.Info()
	return info.Kind, info.Kind != ""
}

func normalizeLifecycleConfig(cfg supervisor.LifecycleConfig) supervisor.LifecycleConfig {
	cfg.InitDefaults()
	dependencies := cfg.RequiredServices()
	sort.Strings(dependencies)
	cfg.Requires = dependencies
	cfg.DependsOn = nil
	return cfg
}

var (
	_ registry.EntryListener = (*Manager)(nil)
	_ api.Registry           = (*Manager)(nil)
)
