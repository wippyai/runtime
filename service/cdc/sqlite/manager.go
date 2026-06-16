// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	config "github.com/wippyai/runtime/api/service/cdc"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

type sourceHandle interface {
	supervisor.Service
	Subscribe(opts config.StreamOptions) config.ChangeStream
	closeSubscriptions()
	markDrop()
}

type sourceOptions struct {
	res            resource.Registry
	log            *zap.Logger
	dbResource     registry.ID
	name           string
	statusInterval string
	tables         []string
	snapshot       bool
}

type Manager struct {
	dtt         payload.Transcoder
	bus         event.Bus
	res         resource.Registry
	log         *zap.Logger
	sources     map[registry.ID]sourceHandle
	infos       map[registry.ID]config.SourceInfo
	infosByName map[string]registry.ID
	mu          sync.Mutex
}

func NewManager(dtt payload.Transcoder, bus event.Bus, log *zap.Logger, res resource.Registry) (*Manager, error) {
	if dtt == nil {
		return nil, ErrTranscoderRequired
	}
	if bus == nil {
		return nil, ErrEventBusRequired
	}
	if res == nil {
		return nil, ErrResourceRegRequired
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &Manager{
		dtt:         dtt,
		bus:         bus,
		res:         res,
		log:         log,
		sources:     make(map[registry.ID]sourceHandle),
		infos:       make(map[registry.ID]config.SourceInfo),
		infosByName: make(map[string]registry.ID),
	}, nil
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry.Kind != config.SQLite {
		return NewUnsupportedEntryKindError(entry.Kind)
	}
	if _, exists := m.sources[entry.ID]; exists {
		return NewServiceExistsError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[config.SQLiteConfig](ctx, m.dtt, entry)
	if err != nil {
		return NewInvalidConfigError(err)
	}
	if err := cfg.Validate(); err != nil {
		return NewInvalidConfigError(err)
	}

	src, err := buildSource(m.sourceOptions(entry, cfg))
	if err != nil {
		return NewSourceCreationError(err)
	}

	m.sources[entry.ID] = src
	m.storeInfo(entry, cfg)
	m.register(ctx, entry, src, cfg.Lifecycle)
	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry.Kind != config.SQLite {
		return NewUnsupportedEntryKindError(entry.Kind)
	}
	if _, exists := m.sources[entry.ID]; !exists {
		return NewServiceNotFoundError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[config.SQLiteConfig](ctx, m.dtt, entry)
	if err != nil {
		return NewInvalidConfigError(err)
	}
	if err := cfg.Validate(); err != nil {
		return NewInvalidConfigError(err)
	}

	src, err := buildSource(m.sourceOptions(entry, cfg))
	if err != nil {
		return NewSourceCreationError(err)
	}

	if old := m.sources[entry.ID]; old != nil {
		old.closeSubscriptions()
	}
	m.removeInfo(entry.ID)
	m.unregister(ctx, entry)

	m.sources[entry.ID] = src
	m.storeInfo(entry, cfg)
	m.register(ctx, entry, src, cfg.Lifecycle)
	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	src, exists := m.sources[entry.ID]
	if !exists {
		return NewServiceNotFoundError(entry.ID)
	}
	src.markDrop()
	src.closeSubscriptions()
	m.removeInfo(entry.ID)
	m.unregister(ctx, entry)
	delete(m.sources, entry.ID)
	return nil
}

func (m *Manager) sourceOptions(entry registry.Entry, cfg *config.SQLiteConfig) sourceOptions {
	return sourceOptions{
		res:            m.res,
		log:            m.log.With(zap.String("id", entry.ID.String())),
		name:           entry.ID.String(),
		dbResource:     registry.ParseID(cfg.DBResource),
		tables:         cfg.Tables,
		statusInterval: cfg.StatusInterval,
		snapshot:       cfg.Snapshot,
	}
}

func (m *Manager) storeInfo(entry registry.Entry, cfg *config.SQLiteConfig) {
	info := config.SourceInfo{
		Name:       entry.ID.String(),
		Engine:     "sqlite",
		DBResource: cfg.DBResource,
		Tables:     append([]string(nil), cfg.Tables...),
		Snapshot:   cfg.Snapshot,
	}
	m.infos[entry.ID] = info
	m.infosByName[info.Name] = entry.ID
}

func (m *Manager) removeInfo(id registry.ID) {
	if info, ok := m.infos[id]; ok {
		if current, present := m.infosByName[info.Name]; present && current == id {
			delete(m.infosByName, info.Name)
		}
		delete(m.infos, id)
	}
}

func (m *Manager) List() []config.SourceInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]config.SourceInfo, 0, len(m.infos))
	for _, info := range m.infos {
		out = append(out, info)
	}
	return out
}

func (m *Manager) Get(name string) (config.SourceInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id, ok := m.infosByName[name]; ok {
		if info, present := m.infos[id]; present {
			return info, true
		}
	}
	return config.SourceInfo{}, false
}

func (m *Manager) Stream(_ context.Context, name string, opts config.StreamOptions) (config.ChangeStream, config.SourceInfo, error) {
	m.mu.Lock()
	src, info, ok := m.lookupSourceLocked(name)
	m.mu.Unlock()
	if !ok {
		return nil, config.SourceInfo{}, NewServiceNotFoundError(registry.ParseID(name))
	}
	return src.Subscribe(opts), info, nil
}

func (m *Manager) lookupSourceLocked(name string) (sourceHandle, config.SourceInfo, bool) {
	if id, ok := m.infosByName[name]; ok {
		if src := m.sources[id]; src != nil {
			return src, m.infos[id], true
		}
	}
	return nil, config.SourceInfo{}, false
}

func (m *Manager) register(ctx context.Context, entry registry.Entry, src sourceHandle, lifecycle supervisor.LifecycleConfig) {
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: src,
			Config:  lifecycle,
		},
	})
	m.log.Info("added sqlite cdc source", zap.String("id", entry.ID.String()), zap.String("kind", entry.Kind))
}

func (m *Manager) unregister(ctx context.Context, entry registry.Entry) {
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   entry.ID.String(),
	})
	m.log.Info("removed sqlite cdc source", zap.String("id", entry.ID.String()))
}
