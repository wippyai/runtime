package temporal

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

const (
	// Registry kind constants
	KindClient      registry.Kind = "temporal.client"
	KindTaskQueue   registry.Kind = "temporal.task_queue"
	KindWorkflowDef registry.Kind = "temporal.workflow_definition"
	KindActivityDef registry.Kind = "temporal.activity_definition"
)

// Manager handles temporal service components
type Manager struct {
	log *zap.Logger
	bus events.Bus
	dtt payload.Transcoder

	clients    *ClientManager
	queues     *TaskQueueManager
	workflows  *WorkflowManager
	activities *ActivityManager
}

// NewManager creates a new temporal service manager
func NewManager(bus events.Bus, dtt payload.Transcoder, logger *zap.Logger) *Manager {
	clients := NewClientManager(logger.Named("clients"))
	queues := NewTaskQueueManager(logger.Named("queues"), clients)
	workflows := NewWorkflowManager(logger.Named("workflows"), queues)
	activities := NewActivityManager(logger.Named("activities"), queues)

	return &Manager{
		log:        logger,
		bus:        bus,
		dtt:        dtt,
		clients:    clients,
		queues:     queues,
		workflows:  workflows,
		activities: activities,
	}
}

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

// Add handles addition of new temporal components
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required")
	}

	switch entry.Kind {
	case KindClient:
		cfg := new(ClientConfig)
		if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
			return err
		}

		if err := m.clients.Add(entry.ID, cfg); err != nil {
			return err
		}

		// Register with supervisor
		client, _ := m.clients.Get(entry.ID)
		m.bus.Send(ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Register,
			Path:   events.Path(entry.ID),
			Data: &supervisor.Entry{
				Service: client,
				Config:  cfg.Lifecycle,
			},
		})

		return nil

	case KindTaskQueue:
		cfg := new(TaskQueueConfig)
		if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
			return err
		}

		if err := m.queues.Add(entry.ID, cfg); err != nil {
			return err
		}

		// Register with supervisor with client dependency
		clientID := registry.ID(cfg.Meta.StringValue(ClientKey))
		cfg.Lifecycle.DependsOn = append(cfg.Lifecycle.DependsOn, string(clientID))

		queue, _ := m.queues.Get(entry.ID)
		m.bus.Send(ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Register,
			Path:   events.Path(entry.ID),
			Data: &supervisor.Entry{
				Service: queue,
				Config:  cfg.Lifecycle,
			},
		})

		return nil

	case KindWorkflowDef:
		cfg := new(WorkflowConfig)
		if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
			return err
		}

		return m.workflows.Add(entry.ID, cfg)

	case KindActivityDef:
		cfg := new(ActivityConfig)
		if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
			return err
		}

		return m.activities.Add(entry.ID, cfg)

	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Update handles updates to existing temporal components
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required")
	}

	switch entry.Kind {
	case KindClient:
		return fmt.Errorf("temporal clients cannot be updated at runtime")

	case KindTaskQueue:
		cfg := new(TaskQueueConfig)
		if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
			return err
		}

		if err := m.queues.Update(entry.ID, cfg); err != nil {
			return err
		}

		// Update supervisor config
		m.bus.Send(ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Update,
			Path:   events.Path(entry.ID),
			Data: &supervisor.Entry{
				Config: cfg.Lifecycle,
			},
		})

		return nil

	case KindWorkflowDef:
		cfg := new(WorkflowConfig)
		if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
			return err
		}

		return m.workflows.Update(entry.ID, cfg)

	case KindActivityDef:
		cfg := new(ActivityConfig)
		if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
			return err
		}

		return m.activities.Update(entry.ID, cfg)

	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Delete handles removal of temporal components
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case KindClient:
		// Check for dependent task queues before deletion
		for _, queue := range m.queues.queues {
			if registry.ID(queue.config.Meta.StringValue(ClientKey)) == entry.ID {
				return fmt.Errorf("client %s is in use by one or more task queues", entry.ID)
			}
		}

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

	case KindTaskQueue:
		// Check for dependent workflows and activities
		dependentActivities := m.activities.FindDependentOnQueue(entry.ID)
		if len(dependentActivities) > 0 {
			return fmt.Errorf("task queue %s has %d dependent activities", entry.ID, len(dependentActivities))
		}

		if err := m.queues.Delete(entry.ID); err != nil {
			return err
		}

		// Unregister from supervisor
		m.bus.Send(ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Remove,
			Path:   events.Path(entry.ID),
		})

		return nil

	case KindWorkflowDef:
		return m.workflows.Delete(entry.ID)

	case KindActivityDef:
		return m.activities.Delete(entry.ID)

	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}
