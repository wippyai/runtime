package temporal

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/temporal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/service/temporal/client"
	"go.uber.org/zap"
)

// Manager handles temporal service registration and lifecycle
type Manager struct {
	log     *zap.Logger
	bus     events.Bus
	dtt     payload.Transcoder
	clients *client.Manager
}

// NewManager creates a new temporal service manager
func NewManager(bus events.Bus, dtt payload.Transcoder, logger *zap.Logger) *Manager {
	return &Manager{
		log:     logger,
		bus:     bus,
		dtt:     dtt,
		clients: client.NewClientManager(logger),
	}
}

// unmarshalAndValidate handles configuration deserialization and validation
func (m *Manager) unmarshalAndValidate(data payload.Payload, cfg interface{}) error {
	if err := m.dtt.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if validator, ok := cfg.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}
	}

	return nil
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required")
	}

	switch entry.Kind {
	case api.KindClient:
		cfg := new(api.ClientConfig)
		if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
			return err
		}

		if err := m.clients.Add(entry.ID, cfg); err != nil {
			return err
		}

		// Create and register service with supervisor
		service, err := m.clients.GetConnection(entry.ID)
		if err != nil {
			return err
		}

		m.bus.Send(ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Register,
			Path:   events.Path(entry.ID),
			Data: &supervisor.Entry{
				Service: service,
				Config:  service.GetLifecycleConfig(), // todo: based on number of references from tasks_queues
			},
		})

		return nil

	case api.KindTaskQueue:
		m.log.Info("task queue registration not implemented", zap.String("id", string(entry.ID)))
		return nil

	case api.KindWorkflow:
		m.log.Info("workflow registration not implemented", zap.String("id", string(entry.ID)))
		return nil

	case api.KindActivity:
		m.log.Info("activity registration not implemented", zap.String("id", string(entry.ID)))
		return nil

	default:
		m.log.Debug("ignoring entry kind", zap.String("kind", string(entry.Kind)))
		return nil
	}
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required")
	}

	switch entry.Kind {
	case api.KindClient:
		return fmt.Errorf("temporal clients cannot be updated at runtime")

	case api.KindTaskQueue:
		m.log.Info("task queue update not implemented", zap.String("id", string(entry.ID)))
		// todo: can change client lifecycle config based on number of references
		return nil

	case api.KindWorkflow:
		m.log.Info("workflow update not implemented", zap.String("id", string(entry.ID)))
		return nil

	case api.KindActivity:
		m.log.Info("activity update not implemented", zap.String("id", string(entry.ID)))
		return nil

	default:
		m.log.Debug("ignoring entry kind", zap.String("kind", string(entry.Kind)))
		return nil
	}
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.KindClient:
		if err := m.clients.Delete(entry.ID); err != nil {
			return err
		}

		// Unregister from supervisor
		m.bus.Send(ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Remove,
			Path:   events.Path(entry.ID),
		})

		return nil

	case api.KindTaskQueue:
		m.log.Info("task queue deletion not implemented", zap.String("id", string(entry.ID)))
		return nil

	case api.KindWorkflow:
		m.log.Info("workflow deletion not implemented", zap.String("id", string(entry.ID)))
		return nil

	case api.KindActivity:
		m.log.Info("activity deletion not implemented", zap.String("id", string(entry.ID)))
		return nil

	default:
		m.log.Debug("ignoring entry kind", zap.String("kind", string(entry.Kind)))
		return nil
	}
}
