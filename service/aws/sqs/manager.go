// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	sqsapi "github.com/wippyai/runtime/api/service/aws/sqs"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	queuesvc "github.com/wippyai/runtime/service/queue"
	"go.uber.org/zap"
)

// Manager handles lifecycle of SQS driver instances.
type Manager struct {
	dtt     payload.Transcoder
	bus     event.Bus
	log     *zap.Logger
	drivers map[registry.ID]*Driver
	mu      sync.RWMutex
}

// NewManager creates a new SQS driver manager.
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
	if entry.Kind != sqsapi.Kind {
		return queuesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.drivers[entry.ID]; exists {
		return queuesvc.NewDriverExistsError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[sqsapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	// Acquire shared AWS config from resource registry (same pattern as S3)
	resourceRegistry := resource.GetRegistry(ctx)
	rsc, err := resourceRegistry.Acquire(ctx, cfg.AWSConfig, resource.ModeNormal)
	if err != nil {
		return queuesvc.NewConfigError("failed to acquire aws config resource", fmt.Errorf("sqs: acquire aws config resource %q: %w", cfg.AWSConfig.String(), err))
	}

	gotConfig, err := rsc.Get()
	if err != nil {
		return queuesvc.NewConfigError("failed to get aws config resource", fmt.Errorf("sqs: get aws config resource: %w", err))
	}

	awsCfg, ok := gotConfig.(aws.Config)
	if !ok {
		return queuesvc.NewConfigError("invalid aws config", fmt.Errorf("sqs: aws config resource is not aws.Config"))
	}

	// Apply SQS-specific endpoint override
	if cfg.Endpoint != "" {
		awsCfg.BaseEndpoint = aws.String(cfg.Endpoint)
	}

	driver := NewDriver(entry.ID, cfg, awsCfg, m.dtt, m.log)
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

	m.log.Info("added sqs driver", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != sqsapi.Kind {
		return queuesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.drivers[entry.ID]; !exists {
		return queuesvc.NewDriverNotFoundError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[sqsapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	resourceRegistry := resource.GetRegistry(ctx)
	rsc, err := resourceRegistry.Acquire(ctx, cfg.AWSConfig, resource.ModeNormal)
	if err != nil {
		return queuesvc.NewConfigError("failed to acquire aws config resource", fmt.Errorf("sqs: acquire aws config resource %q: %w", cfg.AWSConfig.String(), err))
	}

	gotConfig, err := rsc.Get()
	if err != nil {
		return queuesvc.NewConfigError("failed to get aws config resource", fmt.Errorf("sqs: get aws config resource: %w", err))
	}

	awsCfg, ok := gotConfig.(aws.Config)
	if !ok {
		return queuesvc.NewConfigError("invalid aws config", fmt.Errorf("sqs: aws config resource is not aws.Config"))
	}

	if cfg.Endpoint != "" {
		awsCfg.BaseEndpoint = aws.String(cfg.Endpoint)
	}

	// Tear the old driver down before swapping in the replacement: a plain
	// ServiceUpdate would leave the old driver running with stale config.
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

	driver := NewDriver(entry.ID, cfg, awsCfg, m.dtt, m.log)
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

	m.log.Info("updated sqs driver", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != sqsapi.Kind {
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

	m.log.Info("deleted sqs driver", zap.String("id", entry.ID.String()))

	return nil
}
