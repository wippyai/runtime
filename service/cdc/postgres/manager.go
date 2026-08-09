// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/cdc"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/system/entry"
	"go.uber.org/zap"
)

// Manager is the legacy PostgreSQL-specific registry and lifecycle wrapper.
//
// Deprecated: use service/cdc.Manager with NewDriver so source identity and
// lifecycle are owned by the driver-neutral CDC manager.
type Manager struct {
	dtt     payload.Transcoder
	bus     event.Bus
	log     *zap.Logger
	sources map[registry.ID]*Source
	infos   map[registry.ID]config.SourceInfo
	mu      sync.Mutex
}

func NewManager(dtt payload.Transcoder, bus event.Bus, log *zap.Logger) (*Manager, error) {
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
		dtt:     dtt,
		bus:     bus,
		log:     log,
		sources: make(map[registry.ID]*Source),
		infos:   make(map[registry.ID]config.SourceInfo),
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
	if err := cfg.Validate(); err != nil {
		return NewInvalidConfigError(err)
	}
	if err := validateConfigIdentifiers(cfg); err != nil {
		return NewInvalidConfigError(err)
	}
	standby, _ := cfg.StandbyDuration()
	status, _ := cfg.StatusDuration()
	replDSN, adminDSN, err := buildDSNs(cfg)
	if err != nil {
		return err
	}
	src := NewSource(SourceOptions{
		ReplDSN:               replDSN,
		AdminDSN:              adminDSN,
		Name:                  entry.ID.String(),
		Slot:                  cfg.SlotName,
		Publication:           cfg.Publication,
		Tables:                cfg.Tables,
		Temporary:             cfg.Temporary,
		Snapshot:              cfg.Snapshot,
		Streaming:             cfg.Streaming,
		Failover:              cfg.Failover,
		StandbyInterval:       standby,
		StatusInterval:        status,
		SnapshotFetchSize:     cfg.SnapshotFetchSize,
		MaxTransactionChanges: cfg.MaxTransactionChanges,
		MaxTransactionBytes:   cfg.MaxTransactionBytes,
		Log:                   m.log.With(zap.String("id", entry.ID.String())),
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
	if err := cfg.Validate(); err != nil {
		return NewInvalidConfigError(err)
	}
	if err := validateConfigIdentifiers(cfg); err != nil {
		return NewInvalidConfigError(err)
	}
	replDSN, adminDSN, err := buildDSNs(cfg)
	if err != nil {
		return err
	}

	old := m.sources[entry.ID]
	if old != nil {
		old.closeSubscriptions()
	}
	m.removeInfo(entry.ID)
	m.unregister(ctx, entry)

	standby, _ := cfg.StandbyDuration()
	status, _ := cfg.StatusDuration()
	src := NewSource(SourceOptions{
		ReplDSN:               replDSN,
		AdminDSN:              adminDSN,
		Name:                  entry.ID.String(),
		Slot:                  cfg.SlotName,
		Publication:           cfg.Publication,
		Tables:                cfg.Tables,
		Temporary:             cfg.Temporary,
		Snapshot:              cfg.Snapshot,
		Streaming:             cfg.Streaming,
		Failover:              cfg.Failover,
		StandbyInterval:       standby,
		StatusInterval:        status,
		SnapshotFetchSize:     cfg.SnapshotFetchSize,
		MaxTransactionChanges: cfg.MaxTransactionChanges,
		MaxTransactionBytes:   cfg.MaxTransactionBytes,
		Log:                   m.log.With(zap.String("id", entry.ID.String())),
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
	src.closeSubscriptions()
	m.removeInfo(entry.ID)
	m.unregister(ctx, entry)
	delete(m.sources, entry.ID)
	return nil
}

func (m *Manager) storeInfo(entry registry.Entry, cfg *config.Config) {
	info := config.SourceInfo{
		Name:        entry.ID.String(),
		Slot:        cfg.SlotName,
		Publication: cfg.Publication,
		Tables:      append([]string(nil), cfg.Tables...),
		Streaming:   cfg.Streaming,
		Failover:    cfg.Failover,
		Temporary:   cfg.Temporary,
		Snapshot:    cfg.Snapshot,
	}
	m.infos[entry.ID] = info
}

func (m *Manager) removeInfo(id registry.ID) {
	delete(m.infos, id)
}

func (m *Manager) List() []config.SourceInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]config.SourceInfo, 0, len(m.infos))
	for _, info := range m.infos {
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m *Manager) Get(name string) (config.SourceInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := registry.ParseID(name)
	if info, ok := m.infos[id]; ok {
		return info, true
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

func (m *Manager) lookupSourceLocked(name string) (*Source, config.SourceInfo, bool) {
	id := registry.ParseID(name)
	info, ok := m.infos[id]
	if !ok {
		return nil, config.SourceInfo{}, false
	}
	if src := m.sources[id]; src != nil {
		return src, info, true
	}
	return nil, config.SourceInfo{}, false
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

func buildDSNs(cfg *config.Config) (replication, admin string, err error) {
	if cfg.Host == "" {
		return "", "", NewInvalidConfigError(errors.New("resolved host is empty"))
	}
	if cfg.Port <= 0 {
		return "", "", NewInvalidConfigError(fmt.Errorf("resolved port is invalid: %d", cfg.Port))
	}
	if cfg.Username == "" {
		return "", "", NewInvalidConfigError(errors.New("resolved username is empty"))
	}
	if cfg.Database == "" {
		return "", "", NewInvalidConfigError(errors.New("resolved database is empty"))
	}

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

	return replURL.String(), adminURL.String(), nil
}

func optionsQuery(options map[string]string) url.Values {
	q := url.Values{}
	for k, v := range options {
		q.Set(k, v)
	}
	return q
}
