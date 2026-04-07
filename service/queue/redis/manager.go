// SPDX-License-Identifier: MPL-2.0

package redis

import (
	"context"
	"fmt"
	"sync"

	goredis "github.com/redis/go-redis/v9"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	redisapi "github.com/wippyai/runtime/api/service/queue/redis"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	queuesvc "github.com/wippyai/runtime/service/queue"
	"go.uber.org/zap"
)

// Manager handles lifecycle of Redis Streams driver instances.
type Manager struct {
	dtt     payload.Transcoder
	bus     event.Bus
	log     *zap.Logger
	drivers map[registry.ID]*Driver
	mu      sync.RWMutex
}

// NewManager creates a new Redis Streams driver manager.
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
	return &Manager{
		log:     log,
		dtt:     dtt,
		bus:     bus,
		drivers: make(map[registry.ID]*Driver),
	}
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != redisapi.Kind {
		return queuesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.drivers[entry.ID]; exists {
		return queuesvc.NewDriverExistsError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[redisapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	uopts := &goredis.UniversalOptions{
		Addrs:                 cfg.Addrs,
		MasterName:            cfg.MasterName,
		ClientName:            cfg.ClientName,
		Protocol:              cfg.Protocol,
		Username:              cfg.Username,
		Password:              cfg.Password,
		SentinelUsername:      cfg.SentinelUsername,
		SentinelPassword:      cfg.SentinelPassword,
		DB:                    cfg.DB,
		MaxRetries:            cfg.MaxRetries,
		MinRetryBackoff:       cfg.MinRetryBackoff,
		MaxRetryBackoff:       cfg.MaxRetryBackoff,
		DialTimeout:           cfg.DialTimeout,
		ReadTimeout:           cfg.ReadTimeout,
		WriteTimeout:          cfg.WriteTimeout,
		ContextTimeoutEnabled: cfg.ContextTimeoutEnabled,
		PoolSize:              cfg.PoolSize,
		MinIdleConns:          cfg.MinIdleConns,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxActiveConns:        cfg.MaxActiveConns,
		PoolFIFO:              cfg.PoolFIFO,
		PoolTimeout:           cfg.PoolTimeout,
		ConnMaxIdleTime:       cfg.ConnMaxIdleTime,
		ConnMaxLifetime:       cfg.ConnMaxLifetime,
		MaxRedirects:          cfg.MaxRedirects,
		IsClusterMode:         cfg.IsClusterMode,
	}

	if cfg.TLS != nil && cfg.TLS.Enabled {
		tlsCfg, err := cfg.TLS.BuildTLSConfig()
		if err != nil {
			return fmt.Errorf("redis tls config: %w", err)
		}
		uopts.TLSConfig = tlsCfg
	}

	driver := NewDriver(entry.ID, uopts, m.dtt, m.log)
	m.drivers[entry.ID] = driver

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: driver,
			Config:  cfg.Lifecycle,
		},
	})

	m.bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   queueapi.DriverRegister,
		Path:   entry.ID.String(),
		Data:   driver,
	})

	m.log.Info("added redis driver", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != redisapi.Kind {
		return queuesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.RLock()
	driver, exists := m.drivers[entry.ID]
	m.mu.RUnlock()

	if !exists {
		return queuesvc.NewDriverNotFoundError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[redisapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceUpdate,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: driver,
			Config:  cfg.Lifecycle,
		},
	})

	m.log.Info("updated redis driver", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != redisapi.Kind {
		return queuesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.drivers[entry.ID]; !exists {
		return queuesvc.NewDriverNotFoundError(entry.ID)
	}

	delete(m.drivers, entry.ID)

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   entry.ID.String(),
	})

	m.bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   queueapi.DriverDelete,
		Path:   entry.ID.String(),
	})

	m.log.Info("deleted redis driver", zap.String("id", entry.ID.String()))

	return nil
}
