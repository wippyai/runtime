// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"sync"

	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/cdc"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

type Manager struct {
	dtt        payload.Transcoder
	bus        event.Bus
	env        envapi.Registry
	log        *zap.Logger
	sources    map[registry.ID]*Source
	infos      map[registry.ID]config.SourceInfo
	infosByKey map[string]registry.ID
	mu         sync.Mutex
}

func NewManager(dtt payload.Transcoder, bus event.Bus, log *zap.Logger, env envapi.Registry) (*Manager, error) {
	if dtt == nil {
		return nil, ErrTranscoderRequired
	}
	if bus == nil {
		return nil, ErrEventBusRequired
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &Manager{
		dtt:        dtt,
		bus:        bus,
		env:        env,
		log:        log,
		sources:    make(map[registry.ID]*Source),
		infos:      make(map[registry.ID]config.SourceInfo),
		infosByKey: make(map[string]registry.ID),
	}, nil
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry.Kind != config.Postgres {
		return NewUnsupportedEntryKindError(entry.Kind)
	}
	if _, exists := m.sources[entry.ID]; exists {
		return NewServiceExistsError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[config.Config](ctx, m.dtt, entry)
	if err != nil {
		return NewInvalidConfigError(err)
	}
	if err := m.resolveEnv(ctx, cfg); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return NewInvalidConfigError(err)
	}

	standby, _ := cfg.StandbyDuration()
	status, _ := cfg.StatusDuration()
	replDSN, adminDSN := buildDSNs(cfg)
	src := NewSource(SourceOptions{
		ReplDSN:           replDSN,
		AdminDSN:          adminDSN,
		Slot:              cfg.SlotName,
		Publication:       cfg.Publication,
		Tables:            cfg.Tables,
		EventSystem:       cfg.EventSystem,
		Temporary:         cfg.Temporary,
		Snapshot:          cfg.Snapshot,
		Streaming:         cfg.Streaming,
		Failover:          cfg.Failover,
		StandbyInterval:   standby,
		StatusInterval:    status,
		SnapshotFetchSize: cfg.SnapshotFetchSize,
		Bus:               m.bus,
		Log:               m.log.With(zap.String("id", entry.ID.String())),
	})

	m.sources[entry.ID] = src
	m.storeInfo(entry, cfg)
	m.register(ctx, entry, src, cfg.Lifecycle)
	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry.Kind != config.Postgres {
		return NewUnsupportedEntryKindError(entry.Kind)
	}
	if _, exists := m.sources[entry.ID]; !exists {
		return NewServiceNotFoundError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[config.Config](ctx, m.dtt, entry)
	if err != nil {
		return NewInvalidConfigError(err)
	}
	if err := m.resolveEnv(ctx, cfg); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return NewInvalidConfigError(err)
	}

	m.removeInfo(entry.ID)
	m.unregister(ctx, entry)

	standby, _ := cfg.StandbyDuration()
	status, _ := cfg.StatusDuration()
	replDSN, adminDSN := buildDSNs(cfg)
	src := NewSource(SourceOptions{
		ReplDSN:           replDSN,
		AdminDSN:          adminDSN,
		Slot:              cfg.SlotName,
		Publication:       cfg.Publication,
		Tables:            cfg.Tables,
		EventSystem:       cfg.EventSystem,
		Temporary:         cfg.Temporary,
		Snapshot:          cfg.Snapshot,
		Streaming:         cfg.Streaming,
		Failover:          cfg.Failover,
		StandbyInterval:   standby,
		StatusInterval:    status,
		SnapshotFetchSize: cfg.SnapshotFetchSize,
		Bus:               m.bus,
		Log:               m.log.With(zap.String("id", entry.ID.String())),
	})
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
	src.MarkForSlotDrop()
	m.removeInfo(entry.ID)
	m.unregister(ctx, entry)
	delete(m.sources, entry.ID)
	return nil
}

func (m *Manager) storeInfo(entry registry.Entry, cfg *config.Config) {
	info := config.SourceInfo{
		Name:        entry.ID.String(),
		Slot:        cfg.SlotName,
		EventSystem: cfg.EventSystem,
		Publication: cfg.Publication,
		Tables:      append([]string(nil), cfg.Tables...),
		Streaming:   cfg.Streaming,
		Failover:    cfg.Failover,
		Temporary:   cfg.Temporary,
		Snapshot:    cfg.Snapshot,
	}
	if info.EventSystem == "" {
		info.EventSystem = config.DefaultEventSystem
	}
	m.infos[entry.ID] = info
	m.infosByKey[info.Slot] = entry.ID
}

func (m *Manager) removeInfo(id registry.ID) {
	if info, ok := m.infos[id]; ok {
		if current, present := m.infosByKey[info.Slot]; present && current == id {
			delete(m.infosByKey, info.Slot)
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

	if id, ok := m.infosByKey[name]; ok {
		if info, present := m.infos[id]; present {
			return info, true
		}
	}
	for _, info := range m.infos {
		if info.Name == name {
			return info, true
		}
	}
	return config.SourceInfo{}, false
}

func (m *Manager) register(ctx context.Context, entry registry.Entry, src *Source, lifecycle supervisor.LifecycleConfig) {
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: src,
			Config:  lifecycle,
		},
	})
	m.log.Info("added cdc source", zap.String("id", entry.ID.String()), zap.String("kind", entry.Kind))
}

func (m *Manager) unregister(ctx context.Context, entry registry.Entry) {
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   entry.ID.String(),
	})
	m.log.Info("removed cdc source", zap.String("id", entry.ID.String()))
}

func (m *Manager) resolveEnv(ctx context.Context, cfg *config.Config) error {
	if v := m.lookup(ctx, cfg.HostEnv); v != "" {
		cfg.Host = v
	}
	if v := m.lookup(ctx, cfg.PortEnv); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return NewInvalidConfigError(fmt.Errorf("port env %q is not numeric: %w", cfg.PortEnv, err))
		}
		cfg.Port = p
	}
	if v := m.lookup(ctx, cfg.DatabaseEnv); v != "" {
		cfg.Database = v
	}
	if v := m.lookup(ctx, cfg.UsernameEnv); v != "" {
		cfg.Username = v
	}
	if v := m.lookup(ctx, cfg.PasswordEnv); v != "" {
		cfg.Password = v
	}
	return nil
}

func (m *Manager) lookup(ctx context.Context, name string) string {
	if name == "" || m.env == nil {
		return ""
	}
	val, found, err := m.env.Lookup(ctx, name)
	if err != nil || !found {
		return ""
	}
	return val
}

func buildDSNs(cfg *config.Config) (replication, admin string) {
	host := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	base := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.Username, cfg.Password),
		Host:   host,
		Path:   "/" + cfg.Database,
	}

	adminURL := base
	adminURL.RawQuery = optionsQuery(cfg.Options).Encode()

	replURL := base
	q := optionsQuery(cfg.Options)
	q.Set("replication", "database")
	replURL.RawQuery = q.Encode()

	return replURL.String(), adminURL.String()
}

func optionsQuery(options map[string]string) url.Values {
	q := url.Values{}
	for k, v := range options {
		q.Set(k, v)
	}
	return q
}
